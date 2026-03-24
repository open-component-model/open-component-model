package integration_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/registry"
	"golang.org/x/crypto/bcrypt"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/compref"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	"ocm.software/open-component-model/bindings/go/oci/repository/resource"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	ctfrepospec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ocirepospec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/repository/component/resolvers"
	"ocm.software/open-component-model/bindings/go/runtime"

	"ocm.software/open-component-model/bindings/go/transfer"
)

const (
	distributionRegistryImage = "registry:3.0.0"
	testUsername              = "ocm"
	passwordLength            = 20
	charset                   = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
)

// --- test helpers ---

func startRegistry(t *testing.T) (address, user, password string) {
	t.Helper()

	password = generateRandomPassword(t)
	htpasswd := generateHtpasswd(t, testUsername, password)

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	container, err := registry.Run(ctx, distributionRegistryImage,
		registry.WithHtpasswd(htpasswd),
		testcontainers.WithEnv(map[string]string{
			"REGISTRY_VALIDATION_DISABLED": "true",
			"REGISTRY_LOG_LEVEL":           "debug",
		}),
	)
	require.NoError(t, err, "should start registry container")

	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Logf("failed to terminate registry container: %v", err)
		}
	})

	addr, err := container.HostAddress(ctx)
	require.NoError(t, err)

	return addr, testUsername, password
}

func generateRandomPassword(t *testing.T) string {
	t.Helper()
	b := make([]byte, passwordLength)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		require.NoError(t, err)
		b[i] = charset[n.Int64()]
	}
	return string(b)
}

func generateHtpasswd(t *testing.T, username, password string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	require.NoError(t, err)
	return fmt.Sprintf("%s:%s", username, string(hash))
}

func createAuthClient(address, username, password string) *auth.Client {
	return &auth.Client{
		Client:     retry.DefaultClient,
		Credential: auth.StaticCredential(address, auth.Credential{Username: username, Password: password}),
	}
}

// staticCredResolver implements credentials.Resolver for integration tests.
type staticCredResolver struct {
	address  string
	username string
	password string
}

func (s *staticCredResolver) Resolve(_ context.Context, _ runtime.Identity) (map[string]string, error) {
	return map[string]string{
		"username": s.username,
		"password": s.password,
	}, nil
}

// ctfRepoResolver wraps a CTF repository as a ComponentVersionRepositoryResolver.
type ctfRepoResolver struct {
	repo     repository.ComponentVersionRepository
	repoSpec runtime.Typed
}

func (r *ctfRepoResolver) GetRepositorySpecificationForComponent(_ context.Context, _, _ string) (runtime.Typed, error) {
	return r.repoSpec, nil
}

func (r *ctfRepoResolver) GetComponentVersionRepositoryForSpecification(_ context.Context, _ runtime.Typed) (repository.ComponentVersionRepository, error) {
	return r.repo, nil
}

func (r *ctfRepoResolver) GetComponentVersionRepositoryForComponent(_ context.Context, _, _ string) (repository.ComponentVersionRepository, error) {
	return r.repo, nil
}

var _ resolvers.ComponentVersionRepositoryResolver = (*ctfRepoResolver)(nil)

// createCTFRepository creates a CTF-backed OCI repository at the given path.
func createCTFRepository(t *testing.T, path string) repository.ComponentVersionRepository {
	t.Helper()
	fs, err := filesystem.NewFS(path, os.O_RDWR|os.O_CREATE)
	require.NoError(t, err)
	archive := ctf.NewFileSystemCTF(fs)
	store := ocictf.NewFromCTF(archive)
	repo, err := oci.NewRepository(oci.WithResolver(store), oci.WithTempDir(t.TempDir()))
	require.NoError(t, err)
	return repo
}

// --- integration tests ---

func Test_Integration_TransferLocalBlob_CTFToOCI(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	// 1. Start target OCI registry
	registryAddr, user, password := startRegistry(t)

	// 2. Create source CTF with a component version containing a local blob resource
	componentName := "ocm.software/integration-test"
	componentVersion := "1.0.0"
	sourceCTFPath := t.TempDir()

	ctfRepo := createCTFRepository(t, sourceCTFPath)

	resourceData := []byte("Hello, Integration Test!")
	resourceBlob := inmemory.New(bytes.NewReader(resourceData))

	desc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    componentName,
					Version: componentVersion,
				},
			},
			Provider: descriptor.Provider{Name: "test-provider"},
			Resources: []descriptor.Resource{
				{
					ElementMeta: descriptor.ElementMeta{
						ObjectMeta: descriptor.ObjectMeta{Name: "test-resource", Version: "1.0.0"},
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

	// Add resource to CTF — the returned resource has the updated access with localReference filled in
	updatedResource, err := ctfRepo.AddLocalResource(t.Context(), componentName, componentVersion,
		&desc.Component.Resources[0], resourceBlob)
	r.NoError(err)
	desc.Component.Resources[0] = *updatedResource

	// Add component version to CTF
	r.NoError(ctfRepo.AddComponentVersion(t.Context(), desc))

	// 3. Build the transfer graph
	sourceSpec := &ctfrepospec.Repository{
		Type:     runtime.Type{Name: ctfrepospec.Type, Version: ctfrepospec.Version},
		FilePath: sourceCTFPath,
	}

	targetSpec := &ocirepospec.Repository{
		Type:    runtime.Type{Name: ocirepospec.Type, Version: "v1"},
		BaseUrl: fmt.Sprintf("http://%s", registryAddr),
	}

	fromRef := &compref.Ref{
		Repository: sourceSpec,
		Component:  componentName,
		Version:    componentVersion,
	}

	resolver := &ctfRepoResolver{
		repo:     ctfRepo,
		repoSpec: sourceSpec,
	}

	tgd, err := transfer.BuildGraphDefinition(t.Context(), fromRef, targetSpec, resolver)
	r.NoError(err, "graph definition should build successfully")
	r.NotNil(tgd)
	r.NotEmpty(tgd.Transformations)

	// 4. Build and execute the graph
	ctx := t.Context()
	credResolver := &staticCredResolver{address: registryAddr, username: user, password: password}

	repoProvider := provider.NewComponentVersionRepositoryProvider(
		provider.WithTempDir(t.TempDir()),
	)
	resourceRepo := resource.NewResourceRepository(nil)

	b := transfer.NewDefaultBuilder(repoProvider, resourceRepo, credResolver)
	graph, err := b.BuildAndCheck(tgd)
	r.NoError(err, "graph should build and validate")

	r.NoError(graph.Process(ctx), "graph execution should succeed")

	// 5. Verify the component exists in the target registry
	client := createAuthClient(registryAddr, user, password)
	urlRes, err := urlresolver.New(
		urlresolver.WithBaseURL(registryAddr),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(client),
	)
	r.NoError(err)
	targetRepo, err := oci.NewRepository(oci.WithResolver(urlRes), oci.WithTempDir(t.TempDir()))
	r.NoError(err)

	gotDesc, err := targetRepo.GetComponentVersion(ctx, componentName, componentVersion)
	r.NoError(err, "should find transferred component in target registry")
	r.Equal(componentName, gotDesc.Component.Name)
	r.Equal(componentVersion, gotDesc.Component.Version)
	r.Len(gotDesc.Component.Resources, 1)
	r.Equal("test-resource", gotDesc.Component.Resources[0].Name)
}
