package integration_test

import (
	"bytes"
	"compress/gzip"
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
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/log"
	"github.com/testcontainers/testcontainers-go/modules/registry"
	"golang.org/x/crypto/bcrypt"
	orasoci "oras.land/oras-go/v2/content/oci"
	"oras.land/oras-go/v2/errdef"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	"ocm.software/open-component-model/bindings/go/oci/internal/resolver"
	ocmoci "ocm.software/open-component-model/bindings/go/oci/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	"ocm.software/open-component-model/bindings/go/oci/tar"
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

	resolver := resolver.NewURLPathResolver(reg)
	resolver.SetClient(createAuthClient(reg, user, password))

	scheme := ocmruntime.NewScheme()
	ocmoci.MustAddToScheme(scheme)
	v2.MustAddToScheme(scheme)
	scheme.MustRegisterWithAlias(&v2.LocalBlob{}, ocmruntime.NewUnversionedType(v2.LocalBlobAccessType))

	repo, err := oci.NewRepository(oci.WithResolver(resolver), oci.WithScheme(scheme))
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

func Test_Integration_OCIRepository(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	r := require.New(t)

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
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(testcontainers.TerminateContainer(registryContainer))
	})
	t.Logf("Test registry started")

	registryAddress, err := registryContainer.HostAddress(ctx)
	r.NoError(err)

	reference := func(ref string) string {
		return fmt.Sprintf("%s/%s", registryAddress, ref)
	}

	client := createAuthClient(registryAddress, testUsername, password)

	resolver := resolver.NewURLPathResolver(registryAddress)
	resolver.SetClient(client)
	resolver.PlainHTTP = true

	repo, err := oci.NewRepository(oci.WithResolver(resolver))
	r.NoError(err)

	t.Run("basic connectivity and resolution failure", func(t *testing.T) {
		testResolverConnectivity(t, registryAddress, reference("target:latest"), client)
	})

	t.Run("basic upload and download of a component version", func(t *testing.T) {
		uploadDownloadBarebonesComponentVersion(t, repo, "test-component", "v1.0.0")
	})

	t.Run("basic upload and download of a barebones resource that is compatible with OCI registries", func(t *testing.T) {
		uploadDownloadBarebonesOCIImage(t, repo, "ghcr.io/test:v1.0.0", reference("new-test:v1.0.0"))
	})

	t.Run("local resource blob upload and download", func(t *testing.T) {
		uploadDownloadLocalResource(t, repo, "test-component", "v1.0.0")
	})

	t.Run("local resource oci layout upload and download", func(t *testing.T) {
		uploadDownloadLocalResourceOCILayout(t, repo, "test-component", "v1.0.0")
	})
}

func Test_Integration_CTF(t *testing.T) {
	t.Parallel()
	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	require.NoError(t, err)
	archive := ctf.NewFileSystemCTF(fs)
	store := ocictf.NewFromCTF(archive)

	repo, err := oci.NewRepository(oci.WithResolver(store))
	require.NoError(t, err)

	t.Run("basic upload and download of a component version", func(t *testing.T) {
		uploadDownloadBarebonesComponentVersion(t, repo, "test-component", "v1.0.0")
	})

	t.Run("basic upload and download of a barebones resource that is compatible with OCI registries", func(t *testing.T) {
		uploadDownloadBarebonesOCIImage(t, repo, "ghcr.io/test:v1.0.0", "new-test:v1.0.0")
	})

	t.Run("local resource blob upload and download", func(t *testing.T) {
		uploadDownloadLocalResource(t, repo, "test-component", "v2.0.0")
	})

	t.Run("local resource oci layout upload and download", func(t *testing.T) {
		uploadDownloadLocalResourceOCILayout(t, repo, "test-component", "v3.0.0")
	})
}

func uploadDownloadLocalResourceOCILayout(t *testing.T, repo *oci.Repository, component string, version string) {
	ctx := t.Context()
	r := require.New(t)

	originalData := []byte("foobar")

	data, _ := createSingleLayerOCIImage(t, originalData)

	blob := blob.NewDirectReadOnlyBlob(bytes.NewReader(data))

	// Create a simple component descriptor
	cd := &descriptor.Descriptor{
		Meta: descriptor.Meta{
			Version: "v2",
		},
		Component: descriptor.Component{
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

func uploadDownloadBarebonesOCIImage(t *testing.T, repo oci.ResourceRepository, from, to string) {
	ctx := t.Context()
	r := require.New(t)

	originalData := []byte("foobar")

	data, access := createSingleLayerOCIImage(t, originalData, from)

	blob := blob.NewDirectReadOnlyBlob(bytes.NewReader(data))

	resource := descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    "test-resource",
				Version: "v1.0.0",
			},
		},
		Type:         "some-arbitrary-type-packed-in-image",
		Access:       access,
		Size:         int64(len(data)),
		CreationTime: descriptor.CreationTime(time.Now()),
	}

	targetAccess := resource.Access.DeepCopyTyped()
	targetAccess.(*v1.OCIImage).ImageReference = to

	r.NoError(repo.UploadResource(ctx, targetAccess, &resource, blob))

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

func uploadDownloadBarebonesComponentVersion(t *testing.T, repo oci.ComponentVersionRepository, name, version string) {
	ctx := t.Context()
	r := require.New(t)

	desc := descriptor.Descriptor{}
	desc.Component.Name = name
	desc.Component.Version = version
	desc.Component.Labels = append(desc.Component.Labels, descriptor.Label{Name: "foo", Value: "bar"})

	r.NoError(repo.AddComponentVersion(ctx, &desc))

	// Verify that the component version can be retrieved
	retrievedDesc, err := repo.GetComponentVersion(ctx, name, version)
	r.NoError(err)

	r.Equal(name, retrievedDesc.Component.Name)
	r.Equal(version, retrievedDesc.Component.Version)
	r.ElementsMatch(retrievedDesc.Component.Labels, desc.Component.Labels)

	versions, err := repo.ListComponentVersions(ctx, name)
	r.NoError(err)
	r.Len(versions, 1)
	r.Equal(version, versions[0])
}

func testResolverConnectivity(t *testing.T, address, reference string, client *auth.Client) {
	ctx := t.Context()
	r := require.New(t)

	resolver := resolver.NewURLPathResolver(address)
	resolver.SetClient(client)
	resolver.PlainHTTP = true

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
	w := tar.NewOCILayoutWriter(&buf)

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

func uploadDownloadLocalResource(t *testing.T, repo oci.ComponentVersionRepository, name, version string) {
	ctx := t.Context()
	r := require.New(t)

	// Create a simple component descriptor
	cd := &descriptor.Descriptor{
		Meta: descriptor.Meta{
			Version: "v2",
		},
		Component: descriptor.Component{
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

	testBlob := blob.NewDirectReadOnlyBlob(bytes.NewReader(testData))
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

	store, err := tar.ReadOCILayout(t.Context(), downloadedBlob)
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(store.Close())
	})
	r.NotNil(store)
	r.Len(store.Index.Manifests, 1)

}

func getUserAndPasswordWithGitHubCLIAndJQ(t *testing.T) (string, string) {
	t.Helper()
	gh, err := exec.LookPath("gh")
	if err != nil {
		t.Skip("gh CLI not found, skipping test")
	}

	out, err := exec.CommandContext(t.Context(), "sh", "-c", fmt.Sprintf("%s api user", gh)).CombinedOutput()
	if err != nil {
		t.Skipf("gh CLI for user failed: %v", err)
	}
	structured := map[string]interface{}{}
	if err := json.Unmarshal(out, &structured); err != nil {
		t.Skipf("gh CLI for user failed: %v", err)
	}
	user := structured["login"].(string)

	pw := exec.CommandContext(t.Context(), "sh", "-c", fmt.Sprintf("%s auth token", gh))
	if out, err = pw.CombinedOutput(); err != nil {
		t.Skipf("gh CLI for password failed: %v", err)
	}
	password := strings.TrimSpace(string(out))

	return user, password
}
