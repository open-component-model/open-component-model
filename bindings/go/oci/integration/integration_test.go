package integration_test

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/nlepage/go-tarfs"
	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
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
	"ocm.software/open-component-model/bindings/go/oci"
	v1 "ocm.software/open-component-model/bindings/go/oci/access/v1"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	ociDigestV1 "ocm.software/open-component-model/bindings/go/oci/digest/v1"
	"ocm.software/open-component-model/bindings/go/oci/tar"
)

const (
	distributionRegistryImage = "registry:2.8.3"
	testUsername              = "ocm"
	passwordLength            = 20
	charset                   = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*()-_=+[]{}<>?"
	userAgent                 = "ocm.software"
)

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
		testcontainers.WithLogger(testcontainers.TestLogger(t)),
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

	t.Run("basic connectivity and resolution failure", func(t *testing.T) {
		testResolverConnectivity(t, registryAddress, reference("target:latest"), client)
	})

	resolver := oci.NewURLPathResolver(registryAddress)
	resolver.SetClient(client)
	resolver.PlainHTTP = true

	repo, err := oci.NewRepository(oci.WithResolver(resolver))
	r.NoError(err)

	t.Run("basic upload and download of a component version", func(t *testing.T) {
		uploadDownloadBarebonesComponentVersion(t, repo, "test-component", "v1.0.0")
	})
	t.Run("basic upload and download of a barebones resource that is compatible with OCI registries", func(t *testing.T) {
		uploadDownloadBarebonesOCIImage(t, repo, "ghcr.io/test:v1.0.0", reference("new-test:v1.0.0"))
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
}

func uploadDownloadBarebonesOCIImage(t *testing.T, repo oci.ResourceRepository, from, to string) {
	ctx := t.Context()
	r := require.New(t)

	originalData := []byte("foobar")

	data, access := createSingleLayerOCIImage(t, originalData, from)

	dataDigest := digest.FromBytes(data)
	blob := blob.NewDirectReadOnlyBlob(bytes.NewReader(data))

	resource := descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    "test-resource",
				Version: "v1.0.0",
			},
		},
		Type:   "some-arbitrary-type-packed-in-image",
		Access: access,
		Digest: &descriptor.Digest{
			HashAlgorithm:          oci.ReverseHashAlgorithmConversionTable[digest.Canonical],
			NormalisationAlgorithm: ociDigestV1.OCIArtifactDigestAlgorithm,
			Value:                  dataDigest.String(),
		},
		Size:         int64(len(data)),
		CreationTime: descriptor.CreationTime(time.Now()),
	}

	targetAccess := v1.OCIImage{
		ImageReference: to,
	}

	r.NoError(repo.UploadResource(ctx, &targetAccess, &resource, blob))

	resource.Access = &targetAccess

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
	r.Len(retrievedDesc.Component.Labels, 1)
}

func testResolverConnectivity(t *testing.T, address, reference string, client *auth.Client) {
	ctx := t.Context()
	r := require.New(t)

	resolver := oci.NewURLPathResolver(address)
	resolver.SetClient(client)
	resolver.PlainHTTP = true

	store, err := resolver.StoreForReference(ctx, reference)
	r.NoError(err)

	_, err = store.Resolve(ctx, reference)
	r.ErrorIs(err, errdef.ErrNotFound)
	r.ErrorContains(err, fmt.Sprintf("%s: not found", reference))
}

func createAuthClient(address, username, password string) *auth.Client {
	return &auth.Client{
		Client: retry.DefaultClient,
		Header: http.Header{
			"User-Agent": []string{userAgent},
		},
		Credential: auth.StaticCredential(address, auth.Credential{
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

func createSingleLayerOCIImage(t *testing.T, data []byte, ref string) ([]byte, *v1.OCIImage) {
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

	r.NoError(w.Tag(t.Context(), manifestDesc, ref))

	r.NoError(w.Close())

	return buf.Bytes(), &v1.OCIImage{
		ImageReference: ref,
	}
}
