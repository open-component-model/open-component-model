package integration_test

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/registry"
	"golang.org/x/crypto/bcrypt"
	"k8s.io/apimachinery/pkg/types"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	"ocm.software/open-component-model/bindings/go/oci/repository/resource"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	credidentity "ocm.software/open-component-model/bindings/go/oci/spec/identity/v1"
	ocirepospec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer"

	"ocm.software/open-component-model/kubernetes/controller/internal/replication/workerpool"
)

const (
	distributionRegistryImage = "registry:3.0.0"
	testUsername              = "ocm"
	testPassword              = "password"
)

// Test_Integration_WorkerPool_OCIToOCI test the transfer workerpool by creating
// two registries using testcontainers. Creates a component version in the first
// container normally with a resource, then uses the workerpool implementation to
// do a transfer by calling `Submit`. Then, we watch the completion event
// channel from `Events` and wait for the result. Once we got that the pool
// finished the transfer, we call `Result` and verify that it actually worked.
func Test_Integration_WorkerPool_OCIToOCI(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	sourceAddr, sourceUser, sourcePass := startRegistry(t)
	targetAddr, targetUser, targetPass := startRegistry(t)

	const (
		componentName    = "ocm.software/workerpool-e2e"
		componentVersion = "1.0.0"
		resourceName     = "config"
	)
	resourceData := []byte("hello from the worker pool e2e")

	// Seed the source registry directly with a component carrying a local blob.
	sourceRepo := newRegistryRepository(t, sourceAddr, sourceUser, sourcePass)
	desc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{Name: componentName, Version: componentVersion},
			},
			Provider: descriptor.Provider{Name: "test-provider"},
			Resources: []descriptor.Resource{
				{
					ElementMeta: descriptor.ElementMeta{
						ObjectMeta: descriptor.ObjectMeta{Name: resourceName, Version: componentVersion},
					},
					Type:     "plainText",
					Relation: descriptor.LocalRelation,
					Access: &descriptorv2.LocalBlob{
						Type:      runtime.NewVersionedType(descriptorv2.LocalBlobAccessType, descriptorv2.LocalBlobAccessTypeVersion),
						MediaType: "text/plain",
					},
				},
			},
		},
	}

	updatedResource, err := sourceRepo.AddLocalResource(ctx, componentName, componentVersion,
		&desc.Component.Resources[0], inmemory.New(bytes.NewReader(resourceData)))
	r.NoError(err)
	desc.Component.Resources[0] = *updatedResource
	r.NoError(sourceRepo.AddComponentVersion(ctx, desc))

	// Phase 1 (this will happen on the controller side): build the TGD
	sourceSpec := &ocirepospec.Repository{
		Type:    runtime.Type{Name: ocirepospec.Type, Version: "v1"},
		BaseUrl: fmt.Sprintf("http://%s", sourceAddr),
	}
	targetSpec := &ocirepospec.Repository{
		Type:    runtime.Type{Name: ocirepospec.Type, Version: "v1"},
		BaseUrl: fmt.Sprintf("http://%s", targetAddr),
	}

	tgd, err := transfer.BuildGraphDefinition(ctx,
		transfer.WithTransfer(
			transfer.Component(componentName, componentVersion),
			transfer.ToRepositorySpec(targetSpec),
			transfer.FromRepository(sourceRepo, sourceSpec),
		),
	)
	r.NoError(err)
	r.NotEmpty(tgd.Transformations)

	credResolver := newCredResolver(t,
		registryCreds{sourceAddr, sourceUser, sourcePass},
		registryCreds{targetAddr, targetUser, targetPass},
	)
	b := transfer.NewDefaultBuilder(
		provider.NewComponentVersionRepositoryProvider(provider.WithTempDir(t.TempDir())),
		resource.NewResourceRepository(nil),
		credResolver,
	)
	graphBuilder := workerpool.NewGraphBuilder(b)

	// Phase 2: async transfer
	logger := testr.New(t)
	pool := workerpool.NewWorkerPool(workerpool.PoolOptions{
		MaxConcurrentTransfers: 2,
		Logger:                 new(logger),
	})
	events := pool.Events()

	poolCtx, cancelPool := context.WithCancel(ctx)
	poolDone := make(chan struct{})
	go func() {
		defer close(poolDone)
		_ = pool.Start(poolCtx)
	}()
	t.Cleanup(func() {
		cancelPool()
		select {
		case <-poolDone:
		case <-time.After(10 * time.Second):
			t.Error("pool did not shut down in time")
		}
	})

	const (
		key   = "replication-uid-1"
		stamp = componentVersion
	)
	requester := workerpool.RequesterInfo{
		NamespacedName: types.NamespacedName{Namespace: "default", Name: "replication-1"},
	}

	r.ErrorIs(pool.Submit(workerpool.SubmitOptions{
		Key:       key,
		Stamp:     stamp,
		Requester: requester,
		TGD:       tgd,
		Builder:   graphBuilder,
	}), workerpool.ErrTransferInProgress)

	select {
	case got := <-events:
		r.Equal(requester.NamespacedName.Name, got.Object.GetName())
		r.Equal(requester.NamespacedName.Namespace, got.Object.GetNamespace())
	case <-time.After(90 * time.Second):
		t.Fatal("timed out waiting for transfer completion event")
	}

	// Phase 3: finally, verify that the transfer actually worked by doing a GetComponentVersion
	// on the target registry.
	res, ok := pool.Result(key, stamp)
	r.True(ok, "a result matching the desired stamp must be delivered")
	r.NoError(res.Error, "transfer through the worker pool must succeed")
	r.False(pool.IsInProgress(key))

	targetRepo := newRegistryRepository(t, targetAddr, targetUser, targetPass)
	got, err := targetRepo.GetComponentVersion(ctx, componentName, componentVersion)
	r.NoError(err, "transferred component must exist in the target registry")
	r.Equal(componentName, got.Component.Name)
	r.Equal(componentVersion, got.Component.Version)
	r.Len(got.Component.Resources, 1)
	r.Equal(resourceName, got.Component.Resources[0].Name)
}

// newRegistryRepository creates a repository for the given address.
func newRegistryRepository(t *testing.T, addr, user, pass string) repository.ComponentVersionRepository {
	t.Helper()

	urlRes, err := urlresolver.New(
		urlresolver.WithBaseURL(addr),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(&auth.Client{
			Client:     retry.DefaultClient,
			Credential: auth.StaticCredential(addr, auth.Credential{Username: user, Password: pass}),
		}),
	)
	require.NoError(t, err)

	repo, err := oci.NewRepository(oci.WithResolver(urlRes), oci.WithTempDir(t.TempDir()))
	require.NoError(t, err)

	return repo
}

// registryCreds OCI creds.
type registryCreds struct {
	address  string
	username string
	password string
}

// newCredResolver builds a static credentials resolver covering every registry.
// Taken from bindings/go/transfer/integration/integration_test.go.
func newCredResolver(t *testing.T, registries ...registryCreds) *credentials.StaticCredentialsResolver {
	t.Helper()

	credMap := make(map[string]map[string]string, len(registries))
	for _, reg := range registries {
		repo := &ocirepospec.Repository{
			Type:    runtime.Type{Name: ocirepospec.Type, Version: "v1"},
			BaseUrl: fmt.Sprintf("http://%s", reg.address),
		}
		identity, err := credidentity.IdentityFromOCIRepository(repo)
		require.NoError(t, err)
		credMap[identity.String()] = map[string]string{
			"username": reg.username,
			"password": reg.password,
		}
	}

	return credentials.NewStaticCredentialsResolver(credMap)
}

// startRegistry launches an htpasswd-protected OCI registry in a container and
// returns its address and credentials. The container is terminated on cleanup.
func startRegistry(t *testing.T) (address, user, password string) {
	t.Helper()

	htpasswd := generateHtpasswd(t, testUsername, testPassword)

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	container, err := registry.Run(ctx, distributionRegistryImage,
		registry.WithHtpasswd(htpasswd),
		testcontainers.WithEnv(map[string]string{
			"REGISTRY_VALIDATION_DISABLED": "true",
			"REGISTRY_LOG_LEVEL":           "debug",
		}),
	)
	require.NoError(t, err)

	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Logf("failed to terminate registry container: %v", err)
		}
	})

	addr, err := container.HostAddress(ctx)
	require.NoError(t, err)

	return addr, testUsername, testPassword
}

func generateHtpasswd(t *testing.T, username, password string) string {
	t.Helper()

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	require.NoError(t, err)

	return fmt.Sprintf("%s:%s", username, string(hash))
}
