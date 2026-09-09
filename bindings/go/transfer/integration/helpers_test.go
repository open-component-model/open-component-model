package integration_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	godigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/registry"
	"golang.org/x/crypto/bcrypt"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	credidentity "ocm.software/open-component-model/bindings/go/oci/spec/identity/v1"
	ocirepospec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	distributionRegistryImage = "registry:3.0.0"
	testUsername              = "ocm"
	testPassword              = "password"
)

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
	require.NoError(t, err, "should start registry container")

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

func createAuthClient(address, username, password string) *auth.Client {
	return &auth.Client{
		Client:     retry.DefaultClient,
		Credential: auth.StaticCredential(address, auth.Credential{Username: username, Password: password}),
	}
}

// registryCreds holds credentials for a single OCI registry.
type registryCreds struct {
	address  string
	username string
	password string
}

// newCredResolver creates a credentials.StaticCredentialsResolver for one or more OCI registries.
func newCredResolver(t *testing.T, registries ...registryCreds) *credentials.StaticCredentialsResolver {
	t.Helper()
	credMap := make(map[string]map[string]string)
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

// createCTFRepository creates a CTF-backed OCI repository at the given path.
func createCTFRepository(t *testing.T, path string, opts ...oci.RepositoryOption) repository.ComponentVersionRepository {
	t.Helper()
	fs, err := filesystem.NewFS(path, os.O_RDWR|os.O_CREATE)
	require.NoError(t, err)
	archive := ctf.NewFileSystemCTF(fs)
	store := ocictf.NewFromCTF(archive)
	allOpts := append([]oci.RepositoryOption{oci.WithResolver(store), oci.WithTempDir(t.TempDir())}, opts...)
	repo, err := oci.NewRepository(allOpts...)
	require.NoError(t, err)
	return repo
}

// addComponentWithResources creates a component descriptor with local blob resources,
// adds them to the repo, and returns the descriptor.
func addComponentWithResources(t *testing.T, repo repository.ComponentVersionRepository,
	name, version string, resources map[string][]byte,
) *descriptor.Descriptor {
	t.Helper()
	r := require.New(t)

	desc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    name,
					Version: version,
				},
			},
			Provider: descriptor.Provider{Name: "test-provider"},
		},
	}

	for resName, data := range resources {
		res := descriptor.Resource{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{Name: resName, Version: "1.0.0"},
			},
			Type:     "plainText",
			Relation: descriptor.LocalRelation,
			Access: &descriptorv2.LocalBlob{
				Type:      runtime.NewVersionedType(descriptorv2.LocalBlobAccessType, descriptorv2.LocalBlobAccessTypeVersion),
				MediaType: "text/plain",
			},
		}

		updatedResource, err := repo.AddLocalResource(t.Context(), name, version, &res, inmemory.New(bytes.NewReader(data)))
		r.NoError(err)
		desc.Component.Resources = append(desc.Component.Resources, *updatedResource)
	}

	r.NoError(repo.AddComponentVersion(t.Context(), desc))
	return desc
}

func digestOf(content []byte) godigest.Digest {
	return godigest.FromBytes(content)
}
