package repository

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	godigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/s3/internal/download"
	accessspec "ocm.software/open-component-model/bindings/go/s3/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/s3/spec/access/v1"
	credv1 "ocm.software/open-component-model/bindings/go/s3/spec/credentials/v1"
	identityv1 "ocm.software/open-component-model/bindings/go/s3/spec/identity/v1"
)

// fakeS3 is an httptest server answering GetObject with a canned object, so that the
// repository is exercised over the real AWS SDK rather than a stubbed client. It serves
// only a body and a version; the rest of the response is covered in the download package.
type fakeS3 struct {
	*httptest.Server

	body      []byte
	versionID string

	mu       sync.Mutex
	requests []s3Request
}

// s3Request is one request a [fakeS3] answered.
type s3Request struct {
	// path is the request path, which is /bucket/key for path-style addressing.
	path string
	// versionID is the versionId query parameter, empty when none was sent.
	versionID string
}

// newFakeS3 starts a server serving body, reporting versionID as the object version
// when it is not empty, and stops it when the test ends.
func newFakeS3(t *testing.T, body []byte, versionID string) *fakeS3 {
	t.Helper()

	f := &fakeS3{body: body, versionID: versionID}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, s3Request{
			path:      r.URL.Path,
			versionID: r.URL.Query().Get("versionId"),
		})
		f.mu.Unlock()

		if f.versionID != "" {
			w.Header().Set("x-amz-version-id", f.versionID)
		}
		// Real S3 reports a checksum, and the SDK warns about every response carrying none.
		sum := make([]byte, 4)
		binary.BigEndian.PutUint32(sum, crc32.ChecksumIEEE(f.body))
		w.Header().Set("x-amz-checksum-crc32", base64.StdEncoding.EncodeToString(sum))

		_, _ = w.Write(f.body)
	}))
	t.Cleanup(f.Close)

	return f
}

// recorded returns the requests the server answered, in the order it answered them.
func (f *fakeS3) recorded() []s3Request {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]s3Request(nil), f.requests...)
}

// fakeCredentials are static credentials for [fakeS3]. Their value is irrelevant, since
// the fake verifies no signature, but supplying them keeps the AWS default credential
// chain and its instance-metadata lookups out of the tests.
func fakeCredentials() *credv1.S3Credentials {
	return &credv1.S3Credentials{
		Type:            credv1.S3CredentialsVersionedType,
		AccessKeyID:     "test-access-key",
		SecretAccessKey: "test-secret-key",
	}
}

func s3Resource(spec *v1.S3Bucket) *descriptor.Resource {
	spec.Type = accessspec.V1VersionedType
	r := &descriptor.Resource{}
	r.Access = spec
	return r
}

// servedBy points spec at srv. The fake serves no bucket subdomains, so the access is
// addressed path-style.
func servedBy(srv *fakeS3, spec *v1.S3Bucket) *v1.S3Bucket {
	spec.Endpoint = srv.URL
	spec.UsePathStyle = true

	return spec
}

// The identity itself is built by [identityv1.IdentityFromObject] and covered by its
// own tests. What the repository adds is carrying the access spec into it, and
// rejecting an access that names no object before it gets there.
func Test_GetResourceCredentialConsumerIdentity(t *testing.T) {
	repo := NewResourceRepository(nil)

	t.Run("the access spec is carried into the identity", func(t *testing.T) {
		id, err := repo.GetResourceCredentialConsumerIdentity(context.Background(),
			s3Resource(&v1.S3Bucket{BucketName: "b", ObjectKey: "obj", Endpoint: "https://minio.internal:9000"}))
		require.NoError(t, err)
		require.Equal(t, "b/obj", id[runtime.IdentityAttributePath])
		require.Equal(t, "minio.internal", id[runtime.IdentityAttributeHostname])
		require.Equal(t, "9000", id[runtime.IdentityAttributePort])
		require.Equal(t, identityv1.S3BucketIdentityType, id[runtime.IdentityAttributeType])
	})

	tests := []struct {
		name     string
		resource *descriptor.Resource
		wantErr  string
	}{
		{
			name:     "missing object key",
			resource: s3Resource(&v1.S3Bucket{BucketName: "my-bucket"}),
			wantErr:  "objectKey is required",
		},
		{
			name:     "empty access",
			resource: s3Resource(&v1.S3Bucket{}),
			wantErr:  "bucketName is required",
		},
		{
			name:     "no resource",
			resource: nil,
			wantErr:  "resource is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := repo.GetResourceCredentialConsumerIdentity(context.Background(), tt.resource)
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func Test_DownloadResource(t *testing.T) {
	content := []byte("hello from s3")
	tempFolder := t.TempDir()
	srv := newFakeS3(t, content, "")
	repo := NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: &tempFolder})

	b, err := repo.DownloadResource(context.Background(),
		s3Resource(servedBy(srv, &v1.S3Bucket{BucketName: "my-bucket", ObjectKey: "path/blob.txt", Version: "v-1"})),
		fakeCredentials())
	require.NoError(t, err)

	requests := srv.recorded()
	require.Len(t, requests, 1)
	require.Equal(t, "/my-bucket/path/blob.txt", requests[0].path)
	require.Equal(t, "v-1", requests[0].versionID)

	entries, err := os.ReadDir(tempFolder)
	require.NoError(t, err)
	require.Len(t, entries, 1, "the object must be streamed into the configured temp folder")

	rc, err := b.ReadCloser()
	require.NoError(t, err)
	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.NoError(t, rc.Close())
	require.Equal(t, content, got)

	entries, err = os.ReadDir(tempFolder)
	require.NoError(t, err)
	require.Len(t, entries, 1, "the downloaded file is owned by the caller and must outlive the download")
}

func Test_ProcessResourceDigest(t *testing.T) {
	content := []byte("digest me")
	tempFolder := t.TempDir()
	srv := newFakeS3(t, content, "")
	repo := NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: &tempFolder})

	res, err := repo.ProcessResourceDigest(context.Background(),
		s3Resource(servedBy(srv, &v1.S3Bucket{BucketName: "b", ObjectKey: "k"})), fakeCredentials())
	require.NoError(t, err)
	require.NotNil(t, res.Digest)
	require.Equal(t, godigest.FromBytes(content).Encoded(), res.Digest.Value)
	require.Equal(t, hashAlgorithmSHA256, res.Digest.HashAlgorithm)
	require.Equal(t, genericBlobDigestV1, res.Digest.NormalisationAlgorithm)

	entries, err := os.ReadDir(tempFolder)
	require.NoError(t, err)
	require.Empty(t, entries, "digest processing must clean up the object it downloaded")
}

// A hand-written digest cannot know the normalisation algorithm and need not restate the
// hash, so unset fields are filled in and spelling is ignored, matching the github binding.
func Test_ProcessResourceDigest_VerifiesLeniently(t *testing.T) {
	content := []byte("digest me")
	value := godigest.FromBytes(content).Encoded()

	tests := []struct {
		name    string
		digest  *descriptor.Digest
		wantErr string
	}{
		{
			name:   "only the value is given",
			digest: &descriptor.Digest{Value: value},
		},
		{
			name:   "lowercase hash algorithm spelling",
			digest: &descriptor.Digest{HashAlgorithm: "sha-256", NormalisationAlgorithm: genericBlobDigestV1, Value: value},
		},
		{
			name:   "uppercase digest value",
			digest: &descriptor.Digest{Value: strings.ToUpper(value)},
		},
		{
			name:    "a genuinely different hash algorithm is a conflict",
			digest:  &descriptor.Digest{HashAlgorithm: "SHA-512", Value: value},
			wantErr: "hash algorithm mismatch",
		},
		{
			name:    "a genuinely different normalisation algorithm is a conflict",
			digest:  &descriptor.Digest{NormalisationAlgorithm: "ociArtifactDigest/v1", Value: value},
			wantErr: "normalisation algorithm mismatch",
		},
		{
			name:    "a different value is a conflict",
			digest:  &descriptor.Digest{Value: godigest.FromString("something else").Encoded()},
			wantErr: "digest value mismatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newFakeS3(t, content, "")
			tempFolder := t.TempDir()
			repo := NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: &tempFolder})

			resource := s3Resource(servedBy(srv, &v1.S3Bucket{BucketName: "b", ObjectKey: "k"}))
			resource.Digest = tt.digest

			res, err := repo.ProcessResourceDigest(context.Background(), resource, fakeCredentials())
			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)

			// Accepted spellings are canonicalized so descriptors do not vary by author.
			require.Equal(t, hashAlgorithmSHA256, res.Digest.HashAlgorithm)
			require.Equal(t, genericBlobDigestV1, res.Digest.NormalisationAlgorithm)
			require.Equal(t, value, res.Digest.Value)
		})
	}
}

// Test_ProcessResourceDigest_PinsAccess covers the ResourceDigestProcessor requirement
// that a processed access "MUST always reference the content described by the digest
// and cannot be mutated".
func Test_ProcessResourceDigest_PinsAccess(t *testing.T) {
	const versionID = "3sL4kqtJlcpXroDTDmJ+rmSpXd3dIbrHY+MTRCxf3vjVBH40Nr8X8gdRQBpUMLUo"

	tests := []struct {
		name        string
		specVersion string
		versionID   string
		wantVersion string
		wantErr     string
		wantRaw     bool
	}{
		{
			name:        "unpinned access is pinned to the digested version",
			versionID:   versionID,
			wantVersion: versionID,
			wantRaw:     true,
		},
		{
			name:        "an already pinned access is left alone",
			specVersion: "pinned-by-author",
			versionID:   "pinned-by-author",
			wantVersion: "pinned-by-author",
		},
		{
			name:        "a pinned access is kept when the store reports no version",
			specVersion: "pinned-by-author",
			versionID:   "",
			wantVersion: "pinned-by-author",
		},
		{
			// The pinned version is sent as the request's versionId, so a store answering
			// with a different one served an object the access does not name.
			name:        "a store serving a version other than the pinned one is rejected",
			specVersion: "pinned-by-author",
			versionID:   versionID,
			wantErr:     "was requested at version",
		},
		{
			// "null" pins nothing wherever it comes from, so an author who writes it has
			// not pinned the access and the version actually served wins.
			name:        "an author-written null version does not count as pinned",
			specVersion: download.UnversionedVersionID,
			versionID:   versionID,
			wantVersion: versionID,
			wantRaw:     true,
		},
		{
			name:        "an author-written null version is not checked against a null from the store",
			specVersion: download.UnversionedVersionID,
			versionID:   download.UnversionedVersionID,
			wantVersion: download.UnversionedVersionID,
		},
		{
			name:        "unversioned object is left unpinned rather than pinned to null",
			versionID:   download.UnversionedVersionID,
			wantVersion: "",
		},
		{
			name:        "store reporting no version leaves the access unpinned",
			versionID:   "",
			wantVersion: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newFakeS3(t, []byte("digest me"), tt.versionID)
			tempFolder := t.TempDir()
			repo := NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: &tempFolder})

			resource := s3Resource(servedBy(srv, &v1.S3Bucket{BucketName: "b", ObjectKey: "k", Version: tt.specVersion}))
			res, err := repo.ProcessResourceDigest(context.Background(), resource, fakeCredentials())
			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)

			original := v1.S3Bucket{}
			require.NoError(t, accessspec.Scheme.Convert(resource.Access, &original))
			require.Equal(t, tt.specVersion, original.Version, "the caller's resource must not be pinned in place")

			if tt.wantRaw {
				// A rewritten access is handed back raw, because the v2 descriptor encoder
				// cannot encode a typed access it has no scheme for.
				require.IsType(t, &runtime.Raw{}, res.Access)
			}

			spec := v1.S3Bucket{}
			require.NoError(t, accessspec.Scheme.Convert(res.Access, &spec))
			require.Equal(t, tt.wantVersion, spec.Version)

			require.Equal(t, "b", spec.BucketName)
			require.Equal(t, "k", spec.ObjectKey)
			require.NotNil(t, res.Digest)
		})
	}
}

func Test_NewResourceRepository_NilFilesystemConfig(t *testing.T) {
	// Redirect the OS temp dir so the downloaded file is cleaned up with it.
	osTempDir := t.TempDir()
	t.Setenv("TMPDIR", osTempDir)

	content := []byte("no config")
	srv := newFakeS3(t, content, "")
	repo := NewResourceRepository(nil)

	b, err := repo.DownloadResource(context.Background(),
		s3Resource(servedBy(srv, &v1.S3Bucket{BucketName: "b", ObjectKey: "k"})), fakeCredentials())
	require.NoError(t, err)

	rc, err := b.ReadCloser()
	require.NoError(t, err)
	defer func() { _ = rc.Close() }()
	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.Equal(t, content, got)
}
