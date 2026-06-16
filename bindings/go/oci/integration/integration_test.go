package integration_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nlepage/go-tarfs"
	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/log"
	"github.com/testcontainers/testcontainers-go/modules/registry"
	"golang.org/x/crypto/bcrypt"
	"oras.land/oras-go/v2/content"
	orasoci "oras.land/oras-go/v2/content/oci"
	"oras.land/oras-go/v2/errdef"
	orasregistry "oras.land/oras-go/v2/registry"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	filesystemaccess "ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access"
	filesystemaccessv1alpha1 "ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/credentials"
	credconfigv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	"ocm.software/open-component-model/bindings/go/oci/looseref"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	"ocm.software/open-component-model/bindings/go/oci/repository/resource"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	ocispec "ocm.software/open-component-model/bindings/go/oci/spec"
	ocmoci "ocm.software/open-component-model/bindings/go/oci/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/oci/spec/annotations"
	ocicredsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	ctfrepospecv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ocirepospecv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/oci/tar"
	"ocm.software/open-component-model/bindings/go/oci/transformer"
	"ocm.software/open-component-model/bindings/go/repository"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

const (
	distributionRegistryImage = "registry:3.0.0"
	testUsername              = "ocm"
	passwordLength            = 20
	charset                   = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*()-_=+[]{}<>?"
	userAgent                 = "ocm.software"
)

func Test_Integration_OCIRepository_BackwardsCompatibility(t *testing.T) {
	if testing.Short() {
		t.Skipf("skipping integration test as downloading from ghcr.io is taking too long!")
	}

	t.Parallel()
	user, password := getUserAndPasswordWithGitHubCLIAndJQ(t)

	r := require.New(t)
	r.NotEmpty(user)
	r.NotEmpty(password)

	reg := "ghcr.io/open-component-model/ocm"

	resolver, err := urlresolver.New(urlresolver.WithBaseURL(reg))
	r.NoError(err)
	resolver.SetClient(createAuthClient(reg, user, password))

	scheme := ocmruntime.NewScheme()
	ocmoci.MustAddToScheme(scheme)
	v2.MustAddToScheme(scheme)

	repo, err := oci.NewRepository(oci.WithResolver(resolver), oci.WithScheme(scheme), oci.WithTempDir(t.TempDir()))
	r.NoError(err)

	t.Run("basic download of a component version", func(t *testing.T) {
		r := require.New(t)
		component := "ocm.software/ocmcli"

		vs, err := repo.ListComponentVersions(t.Context(), component)
		r.NoError(err)
		r.NotEmpty(vs)

		version := vs[0]

		retrievedDesc, err := repo.GetComponentVersion(t.Context(), component, version)
		r.NoError(err)
		r.NotEmpty(retrievedDesc)

		cliIdentity := ocmruntime.Identity{
			"name":         "ocmcli",
			"os":           runtime.GOOS,
			"architecture": runtime.GOARCH,
		}

		cliDataBlob, _, err := repo.GetLocalResource(t.Context(), component, version, cliIdentity)
		r.NoError(err)
		r.NotNil(cliDataBlob)

		cliPath := filepath.Join(t.TempDir(), "ocm")
		cliFile, err := os.OpenFile(cliPath, os.O_CREATE|os.O_RDWR, 0o744)
		r.NoError(err)
		t.Cleanup(func() {
			err := cliFile.Close()
			if errors.Is(err, os.ErrClosed) {
				return
			}
			r.NoError(err)
		})

		r.NoError(blob.Copy(cliFile, cliDataBlob))
		r.NoError(cliFile.Close())

		out, err := exec.CommandContext(t.Context(), cliPath, "version").CombinedOutput()
		r.NoError(err)
		r.Contains(string(out), version)
	})
}

func Test_Integration_HealthCheck_Authentication(t *testing.T) {
	if testing.Short() {
		t.Skipf("skipping integration test as reaching ghcr.io is taking too long!")
	}

	t.Parallel()
	user, password := getUserAndPasswordWithGitHubCLIAndJQ(t)

	r := require.New(t)
	r.NotEmpty(user)
	r.NotEmpty(password)

	reg := "ghcr.io"

	t.Run("health check with valid authentication succeeds", func(t *testing.T) {
		r := require.New(t)

		resolver, err := urlresolver.New(urlresolver.WithBaseURL(reg))
		r.NoError(err)
		resolver.SetClient(createAuthClient(reg, user, password))

		repo, err := oci.NewRepository(oci.WithResolver(resolver), oci.WithTempDir(t.TempDir()))
		r.NoError(err)

		// Health check should succeed with valid credentials
		err = repo.CheckHealth(t.Context())
		r.NoError(err)
	})

	t.Run("health check without authentication for ghcr.io works", func(t *testing.T) {
		r := require.New(t)

		resolver, err := urlresolver.New(urlresolver.WithBaseURL(reg))
		r.NoError(err)
		// explicitly set default client to avoid token fetch round
		resolver.SetClient(http.DefaultClient)

		repo, err := oci.NewRepository(oci.WithResolver(resolver), oci.WithTempDir(t.TempDir()))
		r.NoError(err)
		r.NoError(repo.CheckHealth(t.Context()))
	})

	t.Run("resolver ping with valid authentication succeeds", func(t *testing.T) {
		r := require.New(t)

		resolver, err := urlresolver.New(urlresolver.WithBaseURL(reg))
		r.NoError(err)
		resolver.SetClient(createAuthClient(reg, user, password))

		// Direct resolver ping should succeed with valid credentials
		err = resolver.Ping(t.Context())
		r.NoError(err)
	})

	t.Run("resolver ping without authentication works", func(t *testing.T) {
		r := require.New(t)

		resolver, err := urlresolver.New(urlresolver.WithBaseURL(reg))
		r.NoError(err)
		resolver.SetClient(http.DefaultClient)

		// Direct resolver ping should pass for ghcr.io without credentials
		// Because we are explicitly ignoring 401 and 403.
		r.NoError(resolver.Ping(t.Context()))
	})
}

func Test_Integration_OCIRepository(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	t.Logf("Starting OCI integration test")

	// Setup credentials and htpasswd
	password := generateRandomPassword(t, passwordLength)
	htpasswd := generateHtpasswd(t, testUsername, password)

	// Start containerized registry
	t.Logf("Launching test registry (%s)...", distributionRegistryImage)
	registryContainer, err := registry.Run(ctx, distributionRegistryImage,
		registry.WithHtpasswd(htpasswd),
		testcontainers.WithEnv(map[string]string{
			"REGISTRY_VALIDATION_DISABLED": "true",
			"REGISTRY_LOG_LEVEL":           "debug",
		}),
		testcontainers.WithLogger(log.TestLogger(t)),
	)
	r := require.New(t)
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(testcontainers.TerminateContainer(registryContainer))
	})
	t.Logf("Test registry started")

	t.Run("direct", func(t *testing.T) {
		r := require.New(t)
		registryAddress, err := registryContainer.HostAddress(ctx)
		r.NoError(err)

		reference := func(ref string) string {
			return fmt.Sprintf("%s/%s", registryAddress, ref)
		}

		client := createAuthClient(registryAddress, testUsername, password)

		resolver, err := urlresolver.New(
			urlresolver.WithBaseURL(registryAddress),
			urlresolver.WithPlainHTTP(true),
			urlresolver.WithBaseClient(client),
		)
		r.NoError(err)

		repo, err := oci.NewRepository(oci.WithResolver(resolver), oci.WithTempDir(t.TempDir()))
		r.NoError(err)

		t.Run("basic connectivity and resolution failure", func(t *testing.T) {
			testResolverConnectivity(t, registryAddress, reference("target:latest"), client)
		})

		t.Run("basic upload and download of a component version", func(t *testing.T) {
			uploadDownloadBarebonesComponentVersion(t, repo, "ocm.software/test-component", "v1.0.0")
		})

		t.Run("basic upload and download of a component with complex version", func(t *testing.T) {
			uploadDownloadBarebonesComponentVersion(t, repo, "test-component-build", "0.0.1-dev-20250121095708.build-b6544da")
		})

		t.Run("basic upload and download of a component with plus character", func(t *testing.T) {
			uploadDownloadBarebonesComponentVersion(t, repo, "test-component-plus", "0.0.2-dev-20250121095708+b6544da")
		})

		t.Run("basic upload and download of a component version (with index based referrer tracking)", func(t *testing.T) {
			repo, err := oci.NewRepository(oci.WithResolver(resolver), oci.WithReferrerTrackingPolicy(oci.ReferrerTrackingPolicyByIndexAndSubject), oci.WithTempDir(t.TempDir()))
			r.NoError(err)
			uploadDownloadBarebonesComponentVersion(t, repo, "ocm.software/test-component", "v1.0.0")
		})

		t.Run("basic upload and download of a barebones resource that is compatible with OCI registries", func(t *testing.T) {
			uploadDownloadBarebonesOCIImage(t, repo, "ghcr.io/test:v1.0.0", reference("new-test:v1.0.0"))
		})

		t.Run("local resource blob upload and download", func(t *testing.T) {
			uploadDownloadLocalResource(t, repo, "ocm.software/test-component", "v1.0.0")
		})

		t.Run("local resource oci layout upload and download", func(t *testing.T) {
			uploadDownloadLocalResourceOCILayout(t, repo, "ocm.software/test-component", "v1.0.0")
		})

		t.Run("local source blob upload and download", func(t *testing.T) {
			uploadDownloadLocalSource(t, repo, "ocm.software/test-component", "v1.0.0")
		})

		t.Run("oci image digest processing", func(t *testing.T) {
			processResourceDigest(t, repo, "ghcr.io/test:v1.0.0", reference("new-test:v1.0.0"))
		})
	})

	t.Run("specification-based", func(t *testing.T) {
		r := require.New(t)

		registryAddress, err := registryContainer.Address(ctx)
		r.NoError(err)

		t.Run("basic connectivity and resolution failure", func(t *testing.T) {
			repoProvider := provider.NewComponentVersionRepositoryProvider()
			repoSpec := &ocirepospecv1.Repository{BaseUrl: registryAddress}
			id, err := repoProvider.GetComponentVersionRepositoryCredentialConsumerIdentity(t.Context(), repoSpec)
			r.NoError(err)

			url, err := ocmruntime.ParseURLAndAllowNoScheme(registryAddress)
			r.NoError(err)
			r.Equal(id[ocmruntime.IdentityAttributeHostname], url.Hostname())
			r.Equal(id[ocmruntime.IdentityAttributePort], url.Port())
			r.Equal(id[ocmruntime.IdentityAttributeScheme], url.Scheme)

			repo, err := repoProvider.GetComponentVersionRepository(ctx, repoSpec, &ocicredsv1.OCICredentials{
				Type:     ocicredsv1.OCICredentialsVersionedType,
				Username: testUsername,
				Password: password,
			})
			r.NoError(err)

			t.Run("basic upload and download of a component version", func(t *testing.T) {
				uploadDownloadBarebonesComponentVersion(t, repo, "ocm.software/test-component", "v1.0.0")
			})
		})
	})
}

func Test_Integration_CTF(t *testing.T) {
	t.Parallel()
	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	require.NoError(t, err)

	t.Run("direct", func(t *testing.T) {
		archive := ctf.NewFileSystemCTF(fs)
		store := ocictf.NewFromCTF(archive)
		repo, err := oci.NewRepository(oci.WithResolver(store), oci.WithTempDir(t.TempDir()))
		require.NoError(t, err)
		t.Run("basic upload and download of a component version", func(t *testing.T) {
			uploadDownloadBarebonesComponentVersion(t, repo, "ocm.software/test-component", "v1.0.0")
		})

		t.Run("basic upload and download of a component with complex version", func(t *testing.T) {
			uploadDownloadBarebonesComponentVersion(t, repo, "test-component-build", "0.0.1-dev-20250121095708.build-b6544da")
		})

		t.Run("basic upload and download of a component with plus character", func(t *testing.T) {
			uploadDownloadBarebonesComponentVersion(t, repo, "test-component-plus", "0.0.2-dev-20250121095708+b6544da")
		})

		t.Run("basic upload and download of a barebones resource that is compatible with OCI registries", func(t *testing.T) {
			uploadDownloadBarebonesOCIImage(t, repo, "ghcr.io/test:v1.0.0", "new-test:v1.0.0")
		})

		t.Run("local resource blob upload and download", func(t *testing.T) {
			uploadDownloadLocalResource(t, repo, "ocm.software/test-component", "v2.0.0")
		})

		t.Run("local resource oci layout upload and download", func(t *testing.T) {
			uploadDownloadLocalResourceOCILayout(t, repo, "ocm.software/test-component", "v3.0.0")
		})

		t.Run("local source blob upload and download", func(t *testing.T) {
			uploadDownloadLocalSource(t, repo, "ocm.software/test-component", "v4.0.0")
		})

		t.Run("oci image digest processing", func(t *testing.T) {
			processResourceDigest(t, repo, "ghcr.io/test:v1.0.0", "new-test:v1.0.0")
		})
	})

	t.Run("specification-based", func(t *testing.T) {
		r := require.New(t)
		repoProvider := provider.NewComponentVersionRepositoryProvider()
		repoSpec := &ctfrepospecv1.Repository{FilePath: fs.String(), AccessMode: ctfrepospecv1.AccessModeReadWrite}
		_, err := repoProvider.GetComponentVersionRepositoryCredentialConsumerIdentity(t.Context(), repoSpec)
		r.Error(err)

		repo, err := repoProvider.GetComponentVersionRepository(t.Context(), repoSpec, nil)
		r.NoError(err)

		t.Run("basic upload and download of a component version", func(t *testing.T) {
			uploadDownloadBarebonesComponentVersion(t, repo, "ocm.software/test-component", "v5.0.0")
		})
	})
}

func Test_Integration_CTF_Lister(t *testing.T) {
	t.Parallel()

	// Test data.
	cvs := []struct {
		name    string
		version string
	}{
		{"github.com/acme.org/helloworld", "v1.0.0"},
		{"github.com/acme.org/helloworld", "v2.0.0"},
		{"github.com/acme.org/helloocm", "v1.0.0"},
		{"github.com/acme.org/hello-open-component-model", "v1.0.0"},
	}

	// Expectation: sorted list, elements are unique.
	expectedList := []string{
		"github.com/acme.org/hello-open-component-model",
		"github.com/acme.org/helloocm",
		"github.com/acme.org/helloworld",
	}

	// Write components to CTF, while validating that everything is written correctly.
	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	require.NoError(t, err)
	archive := ctf.NewFileSystemCTF(fs)
	store := ocictf.NewFromCTF(archive)
	repo, err := oci.NewRepository(oci.WithResolver(store), oci.WithTempDir(t.TempDir()))
	require.NoError(t, err)

	for _, cv := range cvs {
		uploadDownloadBarebonesComponentVersion(t, repo, cv.name, cv.version)
	}

	// Retrieve the component list and check the results.
	lister := ocictf.NewComponentLister(archive)
	result := []string{}
	err = lister.ListComponents(t.Context(), "", func(names []string) error {
		result = append(result, names...)
		return nil
	})

	require.NoError(t, err)
	require.Equal(t, expectedList, result)
}

// Test_Integration_Transformers needs a different setup than the other tests.
// Reason being that transformer.GetOCIArtifact requires repository.ResourceRepository.
// Since oci.Repository does not implement repository.ResourceRepository yet, we could not reuse the setup with
// urlresolver.Resolver. We will be able to unify the testing setup once we refactored the oci Repositories
// https://github.com/open-component-model/ocm-project/issues/774
func Test_Integration_Transformers(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	t.Logf("Starting transformers integration test")

	// Setup credentials and htpasswd
	password := generateRandomPassword(t, passwordLength)
	htpasswd := generateHtpasswd(t, testUsername, password)

	// Start containerized registry
	t.Logf("Launching test registry (%s)...", distributionRegistryImage)
	registryContainer, err := registry.Run(ctx, distributionRegistryImage,
		registry.WithHtpasswd(htpasswd),
		testcontainers.WithEnv(map[string]string{
			"REGISTRY_VALIDATION_DISABLED": "true",
			"REGISTRY_LOG_LEVEL":           "debug",
		}),
		testcontainers.WithLogger(log.TestLogger(t)),
	)
	r := require.New(t)
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(testcontainers.TerminateContainer(registryContainer))
	})
	t.Logf("Test registry started")

	t.Run("ociArtifact", func(t *testing.T) {
		r := require.New(t)
		registryAddress, err := registryContainer.HostAddress(ctx)
		r.NoError(err)

		reference := func(ref string) string {
			return fmt.Sprintf("%s/%s", registryAddress, ref)
		}

		t.Run("get oci artifact", func(t *testing.T) {
			resourceRepo := resource.NewResourceRepository(&filesystemv1alpha1.Config{})

			t.Run("get oci transformation", func(t *testing.T) {
				transformGetOCIArtifact(t, resourceRepo, testUsername, password, "ghcr.io/test:v1.0.0", reference("new-test:v1.0.0"))
			})
		})

		t.Run("add oci artifact", func(t *testing.T) {
			resourceRepo := resource.NewResourceRepository(&filesystemv1alpha1.Config{})

			t.Run("add oci transformation", func(t *testing.T) {
				transformAddOCIArtifact(t, resourceRepo, testUsername, password, reference("add-test:v1.0.0"))
			})
		})
	})
}

func uploadDownloadLocalResourceOCILayout(t *testing.T, repo *oci.Repository, component string, version string) {
	ctx := t.Context()
	r := require.New(t)

	originalData := []byte("foobar")

	data, _ := createSingleLayerOCIImage(t, originalData)

	blob := inmemory.New(bytes.NewReader(data))

	// Create a simple component descriptor
	cd := &descriptor.Descriptor{
		Meta: descriptor.Meta{
			Version: "v2",
		},
		Component: descriptor.Component{
			Provider: descriptor.Provider{
				Name: "ocm.software/open-component-model/bindings/go/oci/integration/test",
			},
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    component,
					Version: version,
				},
			},
		},
	}

	// Create test resource
	resource := &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    "test-resource",
				Version: "v1.0.0",
			},
			ExtraIdentity: map[string]string{
				"type": "test.resource.type",
			},
		},
		Type:     "test.resource.type",
		Relation: descriptor.LocalRelation,
		Access: &v2.LocalBlob{
			Type: ocmruntime.Type{
				Name:    v2.LocalBlobAccessType,
				Version: v2.LocalBlobAccessTypeVersion,
			},
			MediaType:      layout.MediaTypeOCIImageLayoutTarGzipV1,
			LocalReference: digest.FromBytes(data).String(),
		},
	}

	newRes, err := repo.AddLocalResource(ctx, component, version, resource, blob)
	r.NoError(err)

	// Add resource to component descriptor
	cd.Component.Resources = append(cd.Component.Resources, *newRes)

	// Add component version after
	err = repo.AddComponentVersion(ctx, cd)
	r.NoError(err)

	downloaded, newRes, err := repo.GetLocalResource(ctx, component, version, resource.ElementMeta.ToIdentity())
	r.NoError(err)
	r.NotNil(downloaded)

	store, err := tar.ReadOCILayout(t.Context(), downloaded)
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(store.Close())
	})
	r.NotNil(store)
	r.Len(store.Index.Manifests, 1)
}

func uploadDownloadBarebonesOCIImage(t *testing.T, repo *oci.Repository, from, to string) {
	ctx := t.Context()
	r := require.New(t)

	originalData := []byte("foobar")

	data, access := createSingleLayerOCIImage(t, originalData, from)

	blob := inmemory.New(bytes.NewReader(data))

	resource := descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    "test-resource",
				Version: "v1.0.0",
			},
		},
		Type:         "some-arbitrary-type-packed-in-image",
		Access:       access,
		CreationTime: descriptor.CreationTime(time.Now()),
	}

	targetAccess := resource.Access.DeepCopyTyped()
	targetAccess.(*v1.OCIImage).ImageReference = to
	resource.Access = targetAccess

	newRes, err := repo.UploadResource(ctx, &resource, blob)
	r.NoError(err)
	resource = *newRes

	downloaded, err := repo.DownloadResource(ctx, &resource)
	r.NoError(err)

	downloadedDataStream, err := downloaded.ReadCloser()
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(downloadedDataStream.Close())
	})

	unzipped, err := gzip.NewReader(downloadedDataStream)
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(unzipped.Close())
	})
	datafs, err := tarfs.New(unzipped)
	r.NoError(err)

	store, err := orasoci.NewFromFS(ctx, datafs)
	r.NoError(err)

	downloadedManifest, err := store.Resolve(ctx, to)
	r.NoError(err)

	dataStreamFromManifest, err := store.Fetch(ctx, downloadedManifest)
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(dataStreamFromManifest.Close())
	})

	var manifest ociImageSpecV1.Manifest
	r.NoError(json.NewDecoder(dataStreamFromManifest).Decode(&manifest))

	r.Len(manifest.Layers, 1)

	dataStreamFromBlob, err := store.Fetch(ctx, manifest.Layers[0])
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(dataStreamFromBlob.Close())
	})

	dataFromBlob, err := io.ReadAll(dataStreamFromBlob)
	r.NoError(err)

	r.Equal(originalData, dataFromBlob)
}

func processResourceDigest(t *testing.T, repo *oci.Repository, from, to string) {
	ctx := t.Context()
	r := require.New(t)

	originalData := []byte("foobar")

	data, access := createSingleLayerOCIImage(t, originalData, from)

	blob := inmemory.New(bytes.NewReader(data))

	resource := descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    "test-resource",
				Version: "v1.0.0",
			},
		},
		Type:         "some-arbitrary-type-packed-in-image",
		Access:       access,
		CreationTime: descriptor.CreationTime(time.Now()),
	}

	targetAccess := resource.Access.DeepCopyTyped()
	targetAccess.(*v1.OCIImage).ImageReference = to
	resource.Access = targetAccess

	newRes, err := repo.UploadResource(ctx, &resource, blob)
	r.NoError(err)
	resource = *newRes

	r.NotNil(resource.Digest)

	resource.Digest = nil

	newRes, err = repo.ProcessResourceDigest(ctx, &resource)
	r.NoError(err)
	resource = *newRes

	r.Contains(resource.Access.(*v1.OCIImage).ImageReference, "test:v1.0.0@sha256:0aa67467eee1b66c5e549e6b67226e226778f689ccdb46c39fe706b6428c98a5")

	r.Equal(resource.Digest.Value, "0aa67467eee1b66c5e549e6b67226e226778f689ccdb46c39fe706b6428c98a5")
	r.Equal(resource.Digest.HashAlgorithm, "SHA-256")
	r.Equal(resource.Digest.NormalisationAlgorithm, "genericBlobDigest/v1")

	r.NotNil(resource.Digest)
}

func uploadDownloadBarebonesComponentVersion(t *testing.T, repo repository.ComponentVersionRepository, name, version string) {
	ctx := t.Context()
	r := require.New(t)

	desc := descriptor.Descriptor{}
	desc.Component.Name = name
	desc.Component.Version = version
	desc.Component.Labels = append(desc.Component.Labels, descriptor.Label{Name: "foo", Value: []byte(`"bar"`)})
	desc.Component.Provider.Name = "ocm.software/open-component-model/bindings/go/oci/integration/test"

	r.NoError(repo.AddComponentVersion(ctx, &desc))

	// Verify that the component version can be retrieved
	retrievedDesc, err := repo.GetComponentVersion(ctx, name, version)
	r.NoError(err)

	r.Equal(name, retrievedDesc.Component.Name)
	r.Equal(version, retrievedDesc.Component.Version)
	r.ElementsMatch(retrievedDesc.Component.Labels, desc.Component.Labels)

	versions, err := repo.ListComponentVersions(ctx, name)
	r.NoError(err)
	r.Contains(versions, version)
}

func testResolverConnectivity(t *testing.T, address, reference string, client *auth.Client) {
	ctx := t.Context()
	r := require.New(t)

	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(address),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(client),
	)
	r.NoError(err)

	store, err := resolver.StoreForReference(ctx, reference)
	r.NoError(err)

	_, err = store.Resolve(ctx, reference)
	r.ErrorIs(err, errdef.ErrNotFound)
	r.ErrorContains(err, fmt.Sprintf("%s: not found", reference))
}

func createAuthClient(address, username, password string) *auth.Client {
	url, err := ocmruntime.ParseURLAndAllowNoScheme(address)
	if err != nil {
		panic(fmt.Sprintf("invalid address %q: %v", address, err))
	}
	return &auth.Client{
		Client: retry.DefaultClient,
		Header: http.Header{
			"User-Agent": []string{userAgent},
		},
		Credential: auth.StaticCredential(url.Host, auth.Credential{
			Username: username,
			Password: password,
		}),
	}
}

func generateHtpasswd(t *testing.T, username, password string) string {
	t.Helper()
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	require.NoError(t, err)
	return fmt.Sprintf("%s:%s", username, hashedPassword)
}

func generateRandomPassword(t *testing.T, length int) string {
	t.Helper()
	password := make([]byte, length)
	for i := range password {
		randomIndex, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		require.NoError(t, err)
		password[i] = charset[randomIndex.Int64()]
	}
	return string(password)
}

func createSingleLayerOCIImage(t *testing.T, data []byte, ref ...string) ([]byte, *v1.OCIImage) {
	r := require.New(t)
	var buf bytes.Buffer
	w, err := tar.NewOCILayoutWriterWithTempFile(&buf, t.TempDir())
	r.NoError(err)

	desc := ociImageSpecV1.Descriptor{}
	desc.Digest = digest.FromBytes(data)
	desc.Size = int64(len(data))
	desc.MediaType = ociImageSpecV1.MediaTypeImageLayer

	r.NoError(w.Push(t.Context(), desc, bytes.NewReader(data)))

	configRaw, err := json.Marshal(map[string]string{})
	r.NoError(err)
	configDesc := ociImageSpecV1.Descriptor{
		Digest:    digest.FromBytes(configRaw),
		Size:      int64(len(configRaw)),
		MediaType: "application/json",
	}
	r.NoError(w.Push(t.Context(), configDesc, bytes.NewReader(configRaw)))

	manifest := ociImageSpecV1.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Config:    configDesc,
		Layers: []ociImageSpecV1.Descriptor{
			desc,
		},
	}
	manifestRaw, err := json.Marshal(manifest)
	r.NoError(err)

	manifestDesc := ociImageSpecV1.Descriptor{
		Digest:    digest.FromBytes(manifestRaw),
		Size:      int64(len(manifestRaw)),
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
	}
	r.NoError(w.Push(t.Context(), manifestDesc, bytes.NewReader(manifestRaw)))

	for _, ref := range ref {
		r.NoError(w.Tag(t.Context(), manifestDesc, ref))
	}

	r.NoError(w.Close())

	var access *v1.OCIImage

	if len(ref) > 0 {
		access = &v1.OCIImage{
			ImageReference: ref[0],
		}
	}

	return buf.Bytes(), access
}

func uploadDownloadLocalResource(t *testing.T, repo repository.ComponentVersionRepository, name, version string) {
	ctx := t.Context()
	r := require.New(t)

	// Create a simple component descriptor
	cd := &descriptor.Descriptor{
		Meta: descriptor.Meta{
			Version: "v2",
		},
		Component: descriptor.Component{
			Provider: descriptor.Provider{
				Name: "ocm.software/open-component-model/bindings/go/oci/integration/test",
			},
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    name,
					Version: version,
				},
			},
		},
	}

	// Create test data
	testData := []byte("test data")
	testDataDigest := digest.FromBytes(testData)

	// Create test resource
	resource := &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    "test-resource",
				Version: "v1.0.0",
			},
			ExtraIdentity: map[string]string{
				"type": "test.resource.type",
			},
		},
		Type:     "test.resource.type",
		Relation: descriptor.LocalRelation,
		Access: &v2.LocalBlob{
			Type: ocmruntime.Type{
				Name:    v2.LocalBlobAccessType,
				Version: v2.LocalBlobAccessTypeVersion,
			},
			MediaType:      "application/json",
			LocalReference: testDataDigest.String(),
		},
	}

	// Add resource to component descriptor
	cd.Component.Resources = append(cd.Component.Resources, *resource)

	testBlob := inmemory.New(bytes.NewReader(testData))
	testBlob.SetMediaType("application/json")

	// Add local resource
	newRes, err := repo.AddLocalResource(ctx, name, version, resource, testBlob)
	r.NoError(err)
	r.NotNil(newRes)
	cd.Component.Resources[0] = *newRes

	// Add component version after
	err = repo.AddComponentVersion(ctx, cd)
	r.NoError(err)

	// Get local resource
	downloadedBlob, resFromGet, err := repo.GetLocalResource(ctx, name, version, resource.ElementMeta.ToIdentity())
	r.NoError(err)
	r.Equal(resFromGet.ElementMeta, newRes.ElementMeta)

	var data bytes.Buffer
	r.NoError(blob.Copy(&data, downloadedBlob))

	r.Equal(testData, data.Bytes())
}

func uploadDownloadLocalSource(t *testing.T, repo repository.ComponentVersionRepository, name, version string) {
	ctx := t.Context()
	r := require.New(t)

	// Create a simple component descriptor
	cd := &descriptor.Descriptor{
		Meta: descriptor.Meta{
			Version: "v2",
		},
		Component: descriptor.Component{
			Provider: descriptor.Provider{
				Name: "ocm.software/open-component-model/bindings/go/oci/integration/test",
			},
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    name,
					Version: version,
				},
			},
		},
	}

	// Create test data
	testData := []byte("test source data")
	testDataDigest := digest.FromBytes(testData)

	// Create test source
	source := &descriptor.Source{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    "test-source",
				Version: "v1.0.0",
			},
			ExtraIdentity: map[string]string{
				"type": "test.source.type",
			},
		},
		Type: "test.source.type",
		Access: &v2.LocalBlob{
			Type: ocmruntime.Type{
				Name:    v2.LocalBlobAccessType,
				Version: v2.LocalBlobAccessTypeVersion,
			},
			MediaType:      "application/json",
			LocalReference: testDataDigest.String(),
		},
	}

	// Add source to component descriptor
	cd.Component.Sources = append(cd.Component.Sources, *source)

	testBlob := inmemory.New(bytes.NewReader(testData))
	testBlob.SetMediaType("application/json")

	// Add local source
	newSrc, err := repo.AddLocalSource(ctx, name, version, source, testBlob)
	r.NoError(err)
	r.NotNil(newSrc)
	cd.Component.Sources[0] = *newSrc

	// Add component version after
	err = repo.AddComponentVersion(ctx, cd)
	r.NoError(err)

	// Get local source
	downloadedBlob, srcFromGet, err := repo.GetLocalSource(ctx, name, version, source.ElementMeta.ToIdentity())
	r.NoError(err)
	r.Equal(srcFromGet.ElementMeta, newSrc.ElementMeta)

	var data bytes.Buffer
	r.NoError(blob.Copy(&data, downloadedBlob))

	r.Equal(testData, data.Bytes())
}

func getUserAndPasswordWithGitHubCLIAndJQ(t *testing.T) (string, string) {
	t.Helper()
	gh, err := exec.LookPath("gh")
	if err != nil {
		t.Errorf("gh CLI not found, skipping test %v", err)
	}

	user, err := getUsername(t, gh)
	if err != nil {
		t.Errorf("gh CLI for username failed: %v", err)
		return "", ""
	}
	pw := exec.CommandContext(t.Context(), "sh", "-c", fmt.Sprintf("%s auth token", gh))
	out, err := pw.CombinedOutput()
	if err != nil {
		t.Logf("gh auth token output: %s", out)
		t.Errorf("gh CLI for password failed: %v", err)
	}
	password := strings.TrimSpace(string(out))

	return user, password
}

func getUsername(t *testing.T, gh string) (string, error) {
	if githubUser := os.Getenv("GITHUB_USER"); githubUser != "" {
		return githubUser, nil
	}

	out, err := exec.CommandContext(t.Context(), "sh", "-c", fmt.Sprintf("%s api user", gh)).CombinedOutput()
	if err != nil {
		t.Logf("gh CLI output: %s", out)
		return "", fmt.Errorf("gh CLI for user failed: %w", err)
	}
	structured := map[string]interface{}{}
	if err := json.Unmarshal(out, &structured); err != nil {
		t.Logf("gh CLI output: %s", out)
		return "", fmt.Errorf("gh failed to parse output: %w", err)
	}

	return structured["login"].(string), nil
}

func transformGetOCIArtifact(t *testing.T, repo repository.ResourceRepository, username, password, from, to string) {
	ctx := t.Context()
	r := require.New(t)

	url, err := ocmruntime.ParseURLAndAllowNoScheme(to)
	r.NoError(err)

	toIdentity := ocmruntime.Identity{
		"scheme":   "http",
		"hostname": url.Hostname(),
		"port":     url.Port(),
		"type":     "OCIRegistry",
	}

	originalData := []byte("foobar")

	data, access := createSingleLayerOCIImage(t, originalData, from)
	r.NotNil(access)

	access.Type = ocmruntime.Type{
		Name:    "ociArtifact",
		Version: "v1",
	}

	blob := inmemory.New(bytes.NewReader(data))

	resource := descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    "test-resource",
				Version: "v1.0.0",
			},
		},
		Type:         "some-arbitrary-type-packed-in-image",
		Access:       access,
		CreationTime: descriptor.CreationTime(time.Now()),
	}

	targetAccess := resource.Access.DeepCopyTyped()
	targetAccess.(*v1.OCIImage).ImageReference = fmt.Sprintf("http://%s", to)
	resource.Access = targetAccess

	credsMap := map[string]map[string]string{
		toIdentity.String(): {
			"username": username,
			"password": password,
		},
	}
	credsResolver := credentials.NewStaticCredentialsResolver(credsMap)
	creds := credsMap[toIdentity.String()]
	r.NotNil(creds)

	newRes, err := repo.UploadResource(ctx, &resource, blob, &credconfigv1.DirectCredentials{
		Type:       ocmruntime.NewVersionedType(credconfigv1.DirectCredentialsType, credconfigv1.Version),
		Properties: creds,
	})
	r.NoError(err)
	resource = *newRes

	combinedScheme := ocmruntime.NewScheme()
	v2.MustAddToScheme(combinedScheme)
	filesystemaccess.MustAddToScheme(combinedScheme)
	combinedScheme.MustRegisterWithAlias(&v1alpha1.GetOCIArtifact{}, v1alpha1.GetOCIArtifactV1alpha1)

	transform := transformer.GetOCIArtifact{
		Scheme:             combinedScheme,
		Repository:         repo,
		CredentialProvider: credsResolver,
	}

	v2Resource, err := descriptor.ConvertToV2Resource(ocmruntime.NewScheme(ocmruntime.WithAllowUnknown()), newRes)
	r.NoError(err)

	spec := &v1alpha1.GetOCIArtifact{
		Type: ocmruntime.NewVersionedType(v1alpha1.GetOCIArtifactType, v1alpha1.Version),
		ID:   "test-get-oci-transform",
		Spec: &v1alpha1.GetOCIArtifactSpec{
			Resource: v2Resource,
		},
	}

	// Execute transformation
	result, err := transform.Transform(ctx, spec)
	require.NoError(t, err)
	require.NotNil(t, result)

	ociOutput, ok := result.(*v1alpha1.GetOCIArtifact)
	require.True(t, ok)
	require.NotNil(t, ociOutput)

	require.NotNil(t, ociOutput.Output.File)
	require.NotNil(t, ociOutput.Output.File.URI)
	require.Equal(t, ociOutput.Output.File.MediaType, "application/vnd.ocm.software.oci.layout.v1+tar+gzip")
}

func transformAddOCIArtifact(t *testing.T, repo repository.ResourceRepository, username, password, to string) {
	ctx := t.Context()
	r := require.New(t)

	url, err := ocmruntime.ParseURLAndAllowNoScheme(to)
	r.NoError(err)

	toIdentity := ocmruntime.Identity{
		"scheme":   "http",
		"hostname": url.Hostname(),
		"port":     url.Port(),
		"type":     "OCIRegistry",
	}

	originalData := []byte("foobar-add")

	data, access := createSingleLayerOCIImage(t, originalData, "ghcr.io/test-add:v1.0.0")
	r.NotNil(access)

	access.Type = ocmruntime.Type{
		Name:    "ociArtifact",
		Version: "v1",
	}
	access.ImageReference = fmt.Sprintf("http://%s", to)

	// Write OCI image data to a temp file
	tmpFile := filepath.Join(t.TempDir(), "oci-artifact.tar")
	r.NoError(os.WriteFile(tmpFile, data, 0o644))

	resource := descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    "test-resource-add",
				Version: "v1.0.0",
			},
		},
		Type:         "some-arbitrary-type-packed-in-image",
		Access:       access,
		CreationTime: descriptor.CreationTime(time.Now()),
	}

	// Convert to v2 resource for the spec
	v2Resource, err := descriptor.ConvertToV2Resource(ocmruntime.NewScheme(ocmruntime.WithAllowUnknown()), &resource)
	r.NoError(err)

	credsMap := map[string]map[string]string{
		toIdentity.String(): {
			"username": username,
			"password": password,
		},
	}
	credsResolver := credentials.NewStaticCredentialsResolver(credsMap)

	combinedScheme := ocmruntime.NewScheme()
	v2.MustAddToScheme(combinedScheme)
	ocmoci.MustAddToScheme(combinedScheme)
	filesystemaccess.MustAddToScheme(combinedScheme)
	combinedScheme.MustRegisterWithAlias(&v1alpha1.AddOCIArtifact{}, v1alpha1.AddOCIArtifactV1alpha1)

	transform := transformer.AddOCIArtifact{
		Scheme:             combinedScheme,
		Repository:         repo,
		CredentialProvider: credsResolver,
	}

	spec := &v1alpha1.AddOCIArtifact{
		Type: ocmruntime.NewVersionedType(v1alpha1.AddOCIArtifactType, v1alpha1.Version),
		ID:   "test-add-oci-transform",
		Spec: &v1alpha1.AddOCIArtifactSpec{
			Resource: v2Resource,
			File: filesystemaccessv1alpha1.File{
				Type: ocmruntime.NewVersionedType(filesystemaccessv1alpha1.FileType, filesystemaccessv1alpha1.Version),
				URI:  "file://" + tmpFile,
			},
		},
	}

	// Execute transformation
	result, err := transform.Transform(ctx, spec)
	r.NoError(err)
	r.NotNil(result)

	addOutput, ok := result.(*v1alpha1.AddOCIArtifact)
	r.True(ok)
	r.NotNil(addOutput)
	r.NotNil(addOutput.Output)
	r.NotNil(addOutput.Output.Resource)

	// Verify the output resource has the correct name and version
	r.Equal("test-resource-add", addOutput.Output.Resource.Name)
	r.Equal("v1.0.0", addOutput.Output.Resource.Version)

	// Check Access Spec
	rawAccess := addOutput.Output.Resource.Access
	s := ocmruntime.NewScheme(ocmruntime.WithAllowUnknown())

	var resultAccess v1.OCIImage
	err = s.Convert(rawAccess, &resultAccess)
	r.NoError(err)
	r.NotNil(resultAccess)

	r.Equal(fmt.Sprintf("http://%s", to), resultAccess.ImageReference)
	r.Equal("ociArtifact", resultAccess.Type.Name)
	r.Equal("v1", resultAccess.Type.Version)
	r.Equal(fmt.Sprintf("http://%s", to), resultAccess.ImageReference)
}

// Test_Integration_OCIRepository_Ownership exercises the OCI binding's
// ownership-referrer surface (ADR 0016) against both a containerised registry
// and a CTF: AddOwnership creates a single content-addressed referrer per
// subject (idempotent across re-runs), siblings stay isolated, and a
// resource-level transfer (UploadResource) carries the referrer along to the
// target — registry→registry and registry→CTF.
func Test_Integration_OCIRepository_Ownership(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	const (
		component = "ocm.software/test-asset"
		version   = "v1.0.0"
	)

	t.Run("registry", func(t *testing.T) {
		srcResolver, srcReg, srcRepo := startOwnershipRegistry(t, ctx)

		t.Run("by-reference subject", func(t *testing.T) {
			imageRef := pushOwnershipByReferenceImage(t, ctx, srcRepo,
				fmt.Sprintf("%s/test-asset/by-reference:%s", srcReg, version),
				[]byte("by-reference-payload"))

			res := byReferenceResource("backend-image", version, imageRef)
			require.NoError(t, srcRepo.AddOwnership(ctx, component, version, res, nil))

			assertOwnershipReferrerCount(t, ctx, srcResolver, imageRef, 1)
			assertOwnershipReferrerAnnotations(t, ctx, srcResolver, imageRef,
				component, version, "backend-image")
		})

		t.Run("by-value subject", func(t *testing.T) {
			res, subjectRef := addByValueOwnedResource(t, ctx, srcResolver, srcRepo,
				"ocm.software/by-value-asset", version, "backend-image",
				[]byte("registry-by-value-payload"))
			require.NoError(t, srcRepo.AddOwnership(ctx, "ocm.software/by-value-asset", version, res, nil))

			assertOwnershipReferrerCount(t, ctx, srcResolver, subjectRef, 1)
			assertOwnershipReferrerAnnotations(t, ctx, srcResolver, subjectRef,
				"ocm.software/by-value-asset", version, "backend-image")
		})

		t.Run("re-run is idempotent (single content-addressed referrer)", func(t *testing.T) {
			imageRef := pushOwnershipByReferenceImage(t, ctx, srcRepo,
				fmt.Sprintf("%s/test-asset/idempotent:%s", srcReg, version),
				[]byte("idempotent-payload"))

			res := byReferenceResource("backend-image", version, imageRef)
			require.NoError(t, srcRepo.AddOwnership(ctx, component, version, res, nil))
			require.NoError(t, srcRepo.AddOwnership(ctx, component, version, res, nil))

			assertOwnershipReferrerCount(t, ctx, srcResolver, imageRef, 1)
		})

		t.Run("siblings get isolated referrers", func(t *testing.T) {
			const owningComponent = "ocm.software/siblings"
			backendRes, backendSubject := addByValueOwnedResource(t, ctx, srcResolver, srcRepo,
				owningComponent, version, "backend", []byte("siblings-backend"))
			frontendRes, frontendSubject := addByValueOwnedResource(t, ctx, srcResolver, srcRepo,
				owningComponent, version, "frontend", []byte("siblings-frontend"))

			require.NoError(t, srcRepo.AddOwnership(ctx, owningComponent, version, backendRes, nil))
			require.NoError(t, srcRepo.AddOwnership(ctx, owningComponent, version, frontendRes, nil))

			require.NotEqual(t, backendSubject, frontendSubject,
				"siblings must have distinct subject references")
			assertOwnershipReferrerCount(t, ctx, srcResolver, backendSubject, 1)
			assertOwnershipReferrerAnnotations(t, ctx, srcResolver, backendSubject,
				owningComponent, version, "backend")
			assertOwnershipReferrerCount(t, ctx, srcResolver, frontendSubject, 1)
			assertOwnershipReferrerAnnotations(t, ctx, srcResolver, frontendSubject,
				owningComponent, version, "frontend")
		})

		t.Run("multiple owners on the same subject", func(t *testing.T) {
			imageRef := pushOwnershipByReferenceImage(t, ctx, srcRepo,
				fmt.Sprintf("%s/test-asset/multi-owner:%s", srcReg, version),
				[]byte("multi-owner-payload"))

			res := byReferenceResource("backend-image", version, imageRef)
			require.NoError(t, srcRepo.AddOwnership(ctx, "ocm.software/owner-a", version, res, nil))
			require.NoError(t, srcRepo.AddOwnership(ctx, "ocm.software/owner-b", version, res, nil))
			// Re-add owner-a to confirm idempotency holds per (owner, resource) tuple.
			require.NoError(t, srcRepo.AddOwnership(ctx, "ocm.software/owner-a", version, res, nil))

			assertOwnershipReferrerCount(t, ctx, srcResolver, imageRef, 2)
			assertOwnershipReferrerPresent(t, ctx, srcResolver, imageRef,
				"ocm.software/owner-a", version, "backend-image")
			assertOwnershipReferrerPresent(t, ctx, srcResolver, imageRef,
				"ocm.software/owner-b", version, "backend-image")
		})

		t.Run("multiple owners on the same by-value subject", func(t *testing.T) {
			// Cross-component ownership for by-value resources is not yet
			// supported: AddOwnership scopes the subject store by the owning
			// component, so an owner that did not host the resource cannot
			// reach the local-blob manifest. Against a real registry this
			// surfaces as a not-found on resolve.
			res, _ := addByValueOwnedResource(t, ctx, srcResolver, srcRepo,
				"ocm.software/by-value-multi-owner", version, "backend-image",
				[]byte("registry-by-value-multi-owner-payload"))

			err := srcRepo.AddOwnership(ctx, "ocm.software/owner-a", version, res, nil)
			require.ErrorIs(t, err, errdef.ErrNotFound)
		})

		t.Run("transfer registry → registry carries the referrer", func(t *testing.T) {
			// Push image + referrer in src; UploadResource into dst (the binding's
			// resource-level transfer drives ExtendedCopyGraph and copies referrers).
			srcImageRef := pushOwnershipByReferenceImage(t, ctx, srcRepo,
				fmt.Sprintf("%s/test-asset/transfer-src:%s", srcReg, version),
				[]byte("transfer-payload"))
			srcRes := byReferenceResource("backend-image", version, srcImageRef)
			require.NoError(t, srcRepo.AddOwnership(ctx, component, version, srcRes, nil))

			dstResolver, dstReg, dstRepo := startOwnershipRegistry(t, ctx)
			dstImageRef := fmt.Sprintf("%s/test-asset/transfer-dst:%s", dstReg, version)
			transferred := transferByReferenceResource(t, ctx, srcRepo, dstRepo, srcRes, dstImageRef)

			assertOwnershipReferrerCount(t, ctx, dstResolver, transferred, 1)
			assertOwnershipReferrerAnnotations(t, ctx, dstResolver, transferred,
				component, version, "backend-image")
		})

		t.Run("transfer registry → CTF carries the referrer", func(t *testing.T) {
			srcImageRef := pushOwnershipByReferenceImage(t, ctx, srcRepo,
				fmt.Sprintf("%s/test-asset/transfer-ctf-src:%s", srcReg, version),
				[]byte("transfer-ctf-payload"))
			srcRes := byReferenceResource("backend-image", version, srcImageRef)
			require.NoError(t, srcRepo.AddOwnership(ctx, component, version, srcRes, nil))

			ctfResolver, ctfRepo := newCTFOwnershipRepo(t)
			dstImageRef := fmt.Sprintf("ocm.software/test-asset/transfer-ctf-dst:%s", version)
			transferred := transferByReferenceResource(t, ctx, srcRepo, ctfRepo, srcRes, dstImageRef)

			assertOwnershipReferrerCount(t, ctx, ctfResolver, transferred, 1)
			assertOwnershipReferrerAnnotations(t, ctx, ctfResolver, transferred,
				component, version, "backend-image")
		})

		t.Run("streaming transfer registry → registry carries the referrer", func(t *testing.T) {
			// Streaming twin of the registry → registry transfer above. Drives
			// DownloadResourceStream → UploadResourceStream (the path
			// transformer/transfer_oci_artifact.go uses) end-to-end across the
			// wire and proves the ownership referrer rides along the same
			// ExtendedCopyGraph traversal without tar materialization.
			srcImageRef := pushOwnershipByReferenceImage(t, ctx, srcRepo,
				fmt.Sprintf("%s/test-asset/transfer-stream-src:%s", srcReg, version),
				[]byte("transfer-stream-payload"))
			srcRes := byReferenceResource("backend-image", version, srcImageRef)
			require.NoError(t, srcRepo.AddOwnership(ctx, component, version, srcRes, nil))

			dstResolver, dstReg, dstRepo := startOwnershipRegistry(t, ctx)
			dstImageRef := fmt.Sprintf("%s/test-asset/transfer-stream-dst:%s", dstReg, version)
			transferred := transferByReferenceResourceStream(t, ctx, srcRepo, dstRepo, srcRes, dstImageRef)

			assertOwnershipReferrerCount(t, ctx, dstResolver, transferred, 1)
			assertOwnershipReferrerAnnotations(t, ctx, dstResolver, transferred,
				component, version, "backend-image")
		})
	})

	t.Run("ctf", func(t *testing.T) {
		t.Run("by-value subject", func(t *testing.T) {
			ctfResolver, ctfRepo := newCTFOwnershipRepo(t)
			res, subjectRef := addByValueOwnedResource(t, ctx, ctfResolver, ctfRepo,
				component, version, "backend-image", []byte("ctf-by-value-payload"))
			require.NoError(t, ctfRepo.AddOwnership(ctx, component, version, res, nil))

			assertOwnershipReferrerCount(t, ctx, ctfResolver, subjectRef, 1)
			assertOwnershipReferrerAnnotations(t, ctx, ctfResolver, subjectRef,
				component, version, "backend-image")
		})

		t.Run("re-run is idempotent (single content-addressed referrer)", func(t *testing.T) {
			ctfResolver, ctfRepo := newCTFOwnershipRepo(t)
			res, subjectRef := addByValueOwnedResource(t, ctx, ctfResolver, ctfRepo,
				component, version, "backend-image", []byte("ctf-idempotent-payload"))

			require.NoError(t, ctfRepo.AddOwnership(ctx, component, version, res, nil))
			require.NoError(t, ctfRepo.AddOwnership(ctx, component, version, res, nil))

			assertOwnershipReferrerCount(t, ctx, ctfResolver, subjectRef, 1)
		})

		t.Run("siblings get isolated referrers", func(t *testing.T) {
			const owningComponent = "ocm.software/ctf-siblings"
			ctfResolver, ctfRepo := newCTFOwnershipRepo(t)
			backendRes, backendSubject := addByValueOwnedResource(t, ctx, ctfResolver, ctfRepo,
				owningComponent, version, "backend", []byte("ctf-siblings-backend"))
			frontendRes, frontendSubject := addByValueOwnedResource(t, ctx, ctfResolver, ctfRepo,
				owningComponent, version, "frontend", []byte("ctf-siblings-frontend"))

			require.NoError(t, ctfRepo.AddOwnership(ctx, owningComponent, version, backendRes, nil))
			require.NoError(t, ctfRepo.AddOwnership(ctx, owningComponent, version, frontendRes, nil))

			require.NotEqual(t, backendSubject, frontendSubject,
				"siblings must have distinct subject references")
			assertOwnershipReferrerCount(t, ctx, ctfResolver, backendSubject, 1)
			assertOwnershipReferrerAnnotations(t, ctx, ctfResolver, backendSubject,
				owningComponent, version, "backend")
			assertOwnershipReferrerCount(t, ctx, ctfResolver, frontendSubject, 1)
			assertOwnershipReferrerAnnotations(t, ctx, ctfResolver, frontendSubject,
				owningComponent, version, "frontend")
		})

		t.Run("multiple owners on the same subject", func(t *testing.T) {
			// Cross-component ownership for by-value resources is not yet
			// supported: AddOwnership scopes the subject store by the owning
			// component, so an owner that did not host the resource cannot
			// reach the local-blob manifest. Against a CTF the digest is found
			// in the global blob store with media type application/octet-stream,
			// which is not an OCI manifest, so the referrer push is silently
			// skipped.
			ctfResolver, ctfRepo := newCTFOwnershipRepo(t)
			res, subjectRef := addByValueOwnedResource(t, ctx, ctfResolver, ctfRepo,
				component, version, "backend-image", []byte("ctf-multi-owner-payload"))

			require.NoError(t, ctfRepo.AddOwnership(ctx, "ocm.software/owner-a", version, res, nil))
			require.NoError(t, ctfRepo.AddOwnership(ctx, "ocm.software/owner-b", version, res, nil))

			assertOwnershipReferrerCount(t, ctx, ctfResolver, subjectRef, 0)
		})

		t.Run("transfer CTF → registry carries the referrer", func(t *testing.T) {
			// Stage a tagged image in the CTF and attach an ownership referrer
			// to it. Transferring the resource to a registry must carry the
			// referrer along — the binding's UploadResource drives
			// ExtendedCopyGraph and pulls the referrers out of the archive.
			ctfResolver, ctfRepo := newCTFOwnershipRepo(t)
			ctfImageRef := pushOwnershipByReferenceImage(t, ctx, ctfRepo,
				fmt.Sprintf("ocm.software/test-asset/transfer-ctf-src:%s", version),
				[]byte("ctf-to-registry-payload"))
			srcRes := byReferenceResource("backend-image", version, ctfImageRef)
			require.NoError(t, ctfRepo.AddOwnership(ctx, component, version, srcRes, nil))
			assertOwnershipReferrerCount(t, ctx, ctfResolver, ctfImageRef, 1)

			dstResolver, dstReg, dstRepo := startOwnershipRegistry(t, ctx)
			dstImageRef := fmt.Sprintf("%s/test-asset/transfer-ctf-dst:%s", dstReg, version)
			transferred := transferByReferenceResource(t, ctx, ctfRepo, dstRepo, srcRes, dstImageRef)

			assertOwnershipReferrerCount(t, ctx, dstResolver, transferred, 1)
			assertOwnershipReferrerAnnotations(t, ctx, dstResolver, transferred,
				component, version, "backend-image")
		})
	})
}

// Test_Integration_OCIRepository_ReferrerChainTransfer pins that a transfer
// driven by [oras.ExtendedCopyGraph] (via [oci.Repository.UploadResource])
// carries arbitrary-depth referrer chains, not just the immediate referrers
// of the transferred subject. This is independent of OCM ownership semantics:
// we build the chain as raw OCI manifests with [ociImageSpecV1.Manifest.Subject]
// set, then transfer the base image and verify every link survives.
//
//	base ◄── link1 ◄── link2 ◄── link3
func Test_Integration_OCIRepository_ReferrerChainTransfer(t *testing.T) {
	ctx := t.Context()
	const (
		version    = "v1.0.0"
		chainDepth = 3
	)

	srcResolver, srcReg, srcRepo := startOwnershipRegistry(t, ctx)
	dstResolver, dstReg, dstRepo := startOwnershipRegistry(t, ctx)

	srcImageRef := pushOwnershipByReferenceImage(t, ctx, srcRepo,
		fmt.Sprintf("%s/test-asset/chain-src:%s", srcReg, version),
		[]byte("chain-base-payload"))

	srcStore, err := srcResolver.StoreForReference(ctx, srcImageRef)
	require.NoError(t, err)
	srcRef, err := looseref.ParseReference(srcImageRef)
	require.NoError(t, err)
	baseDesc, err := srcStore.Resolve(ctx, srcRef.ReferenceOrTag())
	require.NoError(t, err)

	chain := make([]ociImageSpecV1.Descriptor, 0, chainDepth)
	subject := baseDesc
	for i := 0; i < chainDepth; i++ {
		linkDesc := pushChainLink(t, ctx, srcStore, subject, fmt.Sprintf("ocm.software/test/link-%d", i+1))
		chain = append(chain, linkDesc)
		subject = linkDesc
	}

	srcRes := byReferenceResource("chain-image", version, srcImageRef)
	dstImageRef := fmt.Sprintf("%s/test-asset/chain-dst:%s", dstReg, version)
	transferred := transferByReferenceResource(t, ctx, srcRepo, dstRepo, srcRes, dstImageRef)

	dstStore, err := dstResolver.StoreForReference(ctx, transferred)
	require.NoError(t, err)
	dstGraph, ok := dstStore.(content.ReadOnlyGraphStorage)
	require.Truef(t, ok, "dst store %T must implement content.ReadOnlyGraphStorage", dstStore)
	dstRef, err := looseref.ParseReference(transferred)
	require.NoError(t, err)
	dstBase, err := dstStore.Resolve(ctx, dstRef.ReferenceOrTag())
	require.NoError(t, err)
	require.Equal(t, baseDesc.Digest, dstBase.Digest, "transferred base must keep its digest")

	subjectAtDst := dstBase
	for i, want := range chain {
		refs, err := orasregistry.Referrers(ctx, dstGraph, subjectAtDst, fmt.Sprintf("ocm.software/test/link-%d", i+1))
		require.NoErrorf(t, err, "discover referrers for chain level %d", i+1)
		require.Lenf(t, refs, 1, "chain level %d must surface exactly one referrer at dst", i+1)
		assert.Equalf(t, want.Digest, refs[0].Digest,
			"chain level %d referrer digest at dst must match src", i+1)
		subjectAtDst = refs[0]
	}
}

// pushChainLink writes an empty-payload OCI manifest to store with subject set
// to the given subject and the given artifactType, returning its descriptor.
// Each call produces a unique digest because the artifactType varies.
func pushChainLink(t *testing.T, ctx context.Context, store ocispec.Store, subject ociImageSpecV1.Descriptor, artifactType string) ociImageSpecV1.Descriptor {
	t.Helper()
	r := require.New(t)

	empty := ociImageSpecV1.DescriptorEmptyJSON
	manifest := ociImageSpecV1.Manifest{
		Versioned:    specs.Versioned{SchemaVersion: 2},
		MediaType:    ociImageSpecV1.MediaTypeImageManifest,
		ArtifactType: artifactType,
		Config:       empty,
		Layers:       []ociImageSpecV1.Descriptor{empty},
		Subject:      &subject,
	}
	body, err := json.Marshal(manifest)
	r.NoError(err)
	desc := ociImageSpecV1.Descriptor{
		MediaType:    ociImageSpecV1.MediaTypeImageManifest,
		ArtifactType: artifactType,
		Digest:       digest.FromBytes(body),
		Size:         int64(len(body)),
	}

	if err := store.Push(ctx, empty, bytes.NewReader(empty.Data)); err != nil && !errors.Is(err, errdef.ErrAlreadyExists) {
		r.NoError(err)
	}
	if err := store.Push(ctx, desc, bytes.NewReader(body)); err != nil && !errors.Is(err, errdef.ErrAlreadyExists) {
		r.NoError(err)
	}
	return desc
}

// startOwnershipRegistry boots a fresh htpasswd-protected distribution registry
// and returns a resolver, the registry's host:port, and an oci.Repository
// backed by it. The container is torn down on test cleanup.
func startOwnershipRegistry(t *testing.T, ctx context.Context) (*urlresolver.CachingResolver, string, *oci.Repository) {
	t.Helper()
	r := require.New(t)

	password := generateRandomPassword(t, passwordLength)
	htpasswd := generateHtpasswd(t, testUsername, password)
	registryContainer, err := registry.Run(ctx, distributionRegistryImage,
		registry.WithHtpasswd(htpasswd),
		testcontainers.WithEnv(map[string]string{
			"REGISTRY_VALIDATION_DISABLED": "true",
		}),
		testcontainers.WithLogger(log.TestLogger(t)),
	)
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(testcontainers.TerminateContainer(registryContainer))
	})

	address, err := registryContainer.HostAddress(ctx)
	r.NoError(err)

	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(address),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(createAuthClient(address, testUsername, password)),
	)
	r.NoError(err)

	repo, err := oci.NewRepository(oci.WithResolver(resolver), oci.WithTempDir(t.TempDir()))
	r.NoError(err)
	return resolver, address, repo
}

// newCTFOwnershipRepo returns a CTF-backed resolver and oci.Repository over
// a fresh on-disk archive in t.TempDir().
func newCTFOwnershipRepo(t *testing.T) (*ocictf.Store, *oci.Repository) {
	t.Helper()
	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	require.NoError(t, err)
	store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
	repo, err := oci.NewRepository(ocictf.WithCTF(store), oci.WithTempDir(t.TempDir()))
	require.NoError(t, err)
	return store, repo
}

// pushOwnershipByReferenceImage uploads a one-layer OCI image to imageRef in
// repo via UploadResource (no ownership referrer attached) and returns the
// resolved image reference suitable as a subject for AddOwnership.
func pushOwnershipByReferenceImage(t *testing.T, ctx context.Context, repo *oci.Repository, imageRef string, payload []byte) string {
	t.Helper()
	r := require.New(t)
	data, access := createSingleLayerOCIImage(t, payload, imageRef)
	uploaded, err := repo.UploadResource(ctx, &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "image", Version: "v1.0.0"}},
		Type:        "ociArtifact",
		Access:      access,
	}, inmemory.New(bytes.NewReader(data)))
	r.NoError(err)
	return uploaded.Access.(*v1.OCIImage).ImageReference
}

// byReferenceResource builds a [*descriptor.Resource] with an OCIImage access
// pointing at imageRef.
func byReferenceResource(name, version, imageRef string) *descriptor.Resource {
	return &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: name, Version: version}},
		Type:        "ociArtifact",
		Access: &v1.OCIImage{
			Type:           ocmruntime.NewVersionedType(v1.OCIImageType, v1.Version),
			ImageReference: imageRef,
		},
	}
}

// addByValueOwnedResource adds a single-layer OCI image as a local-blob
// resource on repo and returns the uploaded resource together with the full
// OCI subject reference (component-descriptors repo @ local-blob digest)
// against which an ownership referrer would be discovered.
func addByValueOwnedResource(t *testing.T, ctx context.Context, resolver oci.Resolver, repo *oci.Repository, component, version, resourceName string, payload []byte) (*descriptor.Resource, string) {
	t.Helper()
	r := require.New(t)
	data, _ := createSingleLayerOCIImage(t, payload)
	res, err := repo.AddLocalResource(ctx, component, version, &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: resourceName, Version: version}},
		Type:        "ociArtifact",
		Relation:    descriptor.LocalRelation,
		Access: &v2.LocalBlob{
			Type:           ocmruntime.Type{Name: v2.LocalBlobAccessType, Version: v2.LocalBlobAccessTypeVersion},
			MediaType:      layout.MediaTypeOCIImageLayoutTarGzipV1,
			LocalReference: digest.FromBytes(data).String(),
		},
	}, inmemory.New(bytes.NewReader(data)))
	r.NoError(err)

	ref, err := looseref.ParseReference(resolver.ComponentVersionReference(ctx, component, version))
	r.NoError(err)
	ref.Tag = ""
	ref.Reference.Reference = res.Access.(*v2.LocalBlob).LocalReference
	return res, ref.String()
}

// transferByReferenceResource transfers res from src to dst by downloading the
// resource and uploading it to dstImageRef on dst. The binding's
// UploadResource drives ExtendedCopyGraph and pulls the resource's
// ownership referrers along. Returns the uploaded image reference on dst.
func transferByReferenceResource(t *testing.T, ctx context.Context, src, dst *oci.Repository, res *descriptor.Resource, dstImageRef string) string {
	t.Helper()
	r := require.New(t)
	data, err := src.DownloadResource(ctx, res)
	r.NoError(err)
	target := byReferenceResource(res.Name, res.Version, dstImageRef)
	uploaded, err := dst.UploadResource(ctx, target, data)
	r.NoError(err)
	return uploaded.Access.(*v1.OCIImage).ImageReference
}

// transferByReferenceResourceStream is the streaming twin of
// [transferByReferenceResource]: it transfers res from src to dst via
// DownloadResourceStream → UploadResourceStream (no tar materialization).
// This is the path [transformer/transfer_oci_artifact.go] drives.
func transferByReferenceResourceStream(t *testing.T, ctx context.Context, src, dst *oci.Repository, res *descriptor.Resource, dstImageRef string) string {
	t.Helper()
	r := require.New(t)
	stream, err := src.DownloadResourceStream(ctx, res)
	r.NoError(err)
	target := byReferenceResource(res.Name, res.Version, dstImageRef)
	uploaded, err := dst.UploadResourceStream(ctx, target, stream)
	r.NoError(err)
	return uploaded.Access.(*v1.OCIImage).ImageReference
}

// listOwnershipReferrers walks the OCI Referrers API for the subject
// identified by reference (a full OCI reference, by tag or digest) and
// returns every referrer carrying [annotations.OwnershipArtifactType].
func listOwnershipReferrers(t *testing.T, ctx context.Context, resolver oci.Resolver, reference string) []ociImageSpecV1.Descriptor {
	t.Helper()
	r := require.New(t)
	store, err := resolver.StoreForReference(ctx, reference)
	r.NoError(err)
	graphStore, ok := store.(content.ReadOnlyGraphStorage)
	r.Truef(ok, "store %T must implement content.ReadOnlyGraphStorage for referrers discovery", store)
	ref, err := looseref.ParseReference(reference)
	r.NoError(err)
	subject, err := store.Resolve(ctx, ref.ReferenceOrTag())
	r.NoError(err)
	refs, err := orasregistry.Referrers(ctx, graphStore, subject, annotations.OwnershipArtifactType)
	r.NoError(err)
	return refs
}

// assertOwnershipReferrerCount asserts that exactly want ownership referrers
// (artifact type [annotations.OwnershipArtifactType]) are indexed for subject.
func assertOwnershipReferrerCount(t *testing.T, ctx context.Context, resolver oci.Resolver, subject string, want int) {
	t.Helper()
	got := listOwnershipReferrers(t, ctx, resolver, subject)
	require.Lenf(t, got, want, "subject %q should have %d ownership referrers, got %d", subject, want, len(got))
}

// assertOwnershipReferrerPresent asserts that at least one ownership referrer
// on subjectRef carries the expected (component, version, resource) identity.
// Use when a subject has multiple distinct owners and referrer order is not
// guaranteed.
func assertOwnershipReferrerPresent(t *testing.T, ctx context.Context, resolver oci.Resolver, subjectRef, component, version, resourceName string) {
	t.Helper()
	r := require.New(t)
	for _, ref := range listOwnershipReferrers(t, ctx, resolver, subjectRef) {
		if ref.Annotations[annotations.OwnershipComponentName] != component {
			continue
		}
		if ref.Annotations[annotations.OwnershipComponentVersion] != version {
			continue
		}
		var payload struct {
			Identity map[string]string `json:"identity"`
			Kind     string            `json:"kind"`
		}
		r.NoError(json.Unmarshal([]byte(ref.Annotations[annotations.ArtifactAnnotationKey]), &payload))
		if payload.Kind == "resource" && payload.Identity["name"] == resourceName && payload.Identity["version"] == version {
			return
		}
	}
	t.Fatalf("subject %q missing ownership referrer for component=%s version=%s resource=%s", subjectRef, component, version, resourceName)
}

// assertOwnershipReferrerAnnotations asserts the first ownership referrer on
// subject carries the expected component/version/resource identity in its
// annotations and that its manifest's subject digest matches the resolved
// subject manifest digest (the Referrers API indexes by subject, so a stale
// or wrong subject digest would still appear in the listing).
func assertOwnershipReferrerAnnotations(t *testing.T, ctx context.Context, resolver oci.Resolver, subjectRef, component, version, resourceName string) {
	t.Helper()
	r := require.New(t)
	referrers := listOwnershipReferrers(t, ctx, resolver, subjectRef)
	r.NotEmpty(referrers, "subject %q must carry an ownership referrer", subjectRef)
	ref := referrers[0]

	assert.Equal(t, component, ref.Annotations[annotations.OwnershipComponentName])
	assert.Equal(t, version, ref.Annotations[annotations.OwnershipComponentVersion])

	var payload struct {
		Identity map[string]string `json:"identity"`
		Kind     string            `json:"kind"`
	}
	r.NoError(json.Unmarshal([]byte(ref.Annotations[annotations.ArtifactAnnotationKey]), &payload))
	assert.Equal(t, "resource", payload.Kind)
	assert.Equal(t, resourceName, payload.Identity["name"])
	assert.Equal(t, version, payload.Identity["version"])

	store, err := resolver.StoreForReference(ctx, subjectRef)
	r.NoError(err)
	sref, err := looseref.ParseReference(subjectRef)
	r.NoError(err)
	subject, err := store.Resolve(ctx, sref.ReferenceOrTag())
	r.NoError(err)

	rc, err := store.Fetch(ctx, ref)
	r.NoError(err)
	defer func() { r.NoError(rc.Close()) }()
	var manifest ociImageSpecV1.Manifest
	r.NoError(json.NewDecoder(rc).Decode(&manifest))
	r.NotNil(manifest.Subject, "ownership referrer manifest must carry a subject")
	assert.Equal(t, subject.Digest, manifest.Subject.Digest,
		"referrer subject digest must match the resolved subject manifest digest")
}
