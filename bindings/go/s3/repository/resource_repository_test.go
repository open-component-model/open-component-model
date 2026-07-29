package repository

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	godigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/s3/internal/download"
	accessspec "ocm.software/open-component-model/bindings/go/s3/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/s3/spec/access/v1"
)

// WithClient injects a pre-built S3 client. It lives in this test-only file because
// it is sound only when every resource shares one endpoint, region and identity.
func WithClient(client download.ObjectGetter) Option {
	return func(o *Options) {
		o.client = client
	}
}

// fakeGetter is a stand-in S3 client returning canned object content and recording
// the input it was called with.
type fakeGetter struct {
	body        []byte
	contentType string
	versionID   string
	gotInput    *s3.GetObjectInput
}

func (f *fakeGetter) GetObject(_ context.Context, in *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	f.gotInput = in
	out := &s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(f.body))}
	if f.contentType != "" {
		out.ContentType = aws.String(f.contentType)
	}
	if f.versionID != "" {
		out.VersionId = aws.String(f.versionID)
	}
	return out, nil
}

func s3Resource(spec *v1.S3Bucket) *descriptor.Resource {
	spec.Type = accessspec.V1VersionedType
	r := &descriptor.Resource{}
	r.Access = spec
	return r
}

func Test_GetResourceCredentialConsumerIdentity(t *testing.T) {
	repo := NewResourceRepository(nil)

	id, err := repo.GetResourceCredentialConsumerIdentity(context.Background(),
		s3Resource(&v1.S3Bucket{BucketName: "my-bucket", ObjectKey: "path/to/blob", Region: "eu-central-1"}))
	require.NoError(t, err)
	require.Empty(t, id[runtime.IdentityAttributeHostname])
	require.Empty(t, id[runtime.IdentityAttributeScheme])
	require.Equal(t, "my-bucket/path/to/blob", id[runtime.IdentityAttributePath])
	require.Equal(t, accessspec.S3BucketConsumerType, id[runtime.IdentityAttributeType])

	id, err = repo.GetResourceCredentialConsumerIdentity(context.Background(),
		s3Resource(&v1.S3Bucket{BucketName: "my-bucket"}))
	require.NoError(t, err)
	require.Empty(t, id[runtime.IdentityAttributeHostname])
	require.Equal(t, "my-bucket", id[runtime.IdentityAttributePath])

	id, err = repo.GetResourceCredentialConsumerIdentity(context.Background(),
		s3Resource(&v1.S3Bucket{BucketName: "b", ObjectKey: "obj", Endpoint: "https://minio.internal:9000"}))
	require.NoError(t, err)
	require.Equal(t, "minio.internal", id[runtime.IdentityAttributeHostname])
	require.Equal(t, "9000", id[runtime.IdentityAttributePort])
	require.Equal(t, "b/obj", id[runtime.IdentityAttributePath])

	_, err = repo.GetResourceCredentialConsumerIdentity(context.Background(), s3Resource(&v1.S3Bucket{}))
	require.Error(t, err)
	_, err = repo.GetResourceCredentialConsumerIdentity(context.Background(), nil)
	require.Error(t, err)
}

func Test_DownloadResource(t *testing.T) {
	content := []byte("hello from s3")
	fake := &fakeGetter{body: content, contentType: "text/plain"}
	repo := NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: t.TempDir()}, WithClient(fake))

	b, err := repo.DownloadResource(context.Background(),
		s3Resource(&v1.S3Bucket{BucketName: "my-bucket", ObjectKey: "path/blob.txt", Version: "v-1"}), nil)
	require.NoError(t, err)

	rc, err := b.ReadCloser()
	require.NoError(t, err)
	defer func() { _ = rc.Close() }()
	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.Equal(t, content, got)

	require.Equal(t, "my-bucket", aws.ToString(fake.gotInput.Bucket))
	require.Equal(t, "path/blob.txt", aws.ToString(fake.gotInput.Key))
	require.Equal(t, "v-1", aws.ToString(fake.gotInput.VersionId))
}

func Test_ProcessResourceDigest(t *testing.T) {
	content := []byte("digest me")
	tempFolder := t.TempDir()
	repo := NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: tempFolder},
		WithClient(&fakeGetter{body: content}))

	res, err := repo.ProcessResourceDigest(context.Background(),
		s3Resource(&v1.S3Bucket{BucketName: "b", ObjectKey: "k"}), nil)
	require.NoError(t, err)
	require.NotNil(t, res.Digest)
	require.Equal(t, godigest.FromBytes(content).Encoded(), res.Digest.Value)
	require.Equal(t, hashAlgorithmSHA256, res.Digest.HashAlgorithm)
	require.Equal(t, genericBlobDigestV1, res.Digest.NormalisationAlgorithm)

	entries, err := os.ReadDir(tempFolder)
	require.NoError(t, err)
	require.Empty(t, entries, "digest processing must clean up the object it downloaded")
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
	}{
		{
			name:        "unpinned access is pinned to the digested version",
			versionID:   versionID,
			wantVersion: versionID,
		},
		{
			name:        "an already pinned access is left alone",
			specVersion: "pinned-by-author",
			versionID:   versionID,
			wantVersion: "pinned-by-author",
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
			repo := NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: t.TempDir()},
				WithClient(&fakeGetter{body: []byte("digest me"), versionID: tt.versionID}))

			resource := s3Resource(&v1.S3Bucket{BucketName: "b", ObjectKey: "k", Version: tt.specVersion})
			res, err := repo.ProcessResourceDigest(context.Background(), resource, nil)
			require.NoError(t, err)

			spec := v1.S3Bucket{}
			require.NoError(t, accessspec.Scheme.Convert(res.Access, &spec))
			require.Equal(t, tt.wantVersion, spec.Version)

			require.Equal(t, "b", spec.BucketName)
			require.Equal(t, "k", spec.ObjectKey)
			require.NotNil(t, res.Digest)
		})
	}
}

func Test_ProcessResourceDigest_DoesNotMutateInputResource(t *testing.T) {
	repo := NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: t.TempDir()},
		WithClient(&fakeGetter{body: []byte("digest me"), versionID: "v-99"}))

	resource := s3Resource(&v1.S3Bucket{BucketName: "b", ObjectKey: "k"})
	res, err := repo.ProcessResourceDigest(context.Background(), resource, nil)
	require.NoError(t, err)

	original := v1.S3Bucket{}
	require.NoError(t, accessspec.Scheme.Convert(resource.Access, &original))
	require.Empty(t, original.Version, "the caller's resource must not be pinned in place")

	pinned := v1.S3Bucket{}
	require.NoError(t, accessspec.Scheme.Convert(res.Access, &pinned))
	require.Equal(t, "v-99", pinned.Version)
}

func Test_DownloadResource_StreamsIntoConfiguredTempFolder(t *testing.T) {
	content := []byte("hello from s3")
	tempFolder := t.TempDir()
	repo := NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: tempFolder},
		WithClient(&fakeGetter{body: content}))

	b, err := repo.DownloadResource(context.Background(),
		s3Resource(&v1.S3Bucket{BucketName: "b", ObjectKey: "k"}), nil)
	require.NoError(t, err)

	entries, err := os.ReadDir(tempFolder)
	require.NoError(t, err)
	require.Len(t, entries, 1, "the object must be streamed into the configured temp folder")

	rc, err := b.ReadCloser()
	require.NoError(t, err)
	defer func() { _ = rc.Close() }()
	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.Equal(t, content, got)
}

func Test_DownloadResource_BlobOwnsItsFile(t *testing.T) {
	tempFolder := t.TempDir()
	repo := NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: tempFolder},
		WithClient(&fakeGetter{body: []byte("hello from s3")}))

	b, err := repo.DownloadResource(context.Background(),
		s3Resource(&v1.S3Bucket{BucketName: "b", ObjectKey: "k"}), nil)
	require.NoError(t, err)

	entries, err := os.ReadDir(tempFolder)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	closer, ok := b.(io.Closer)
	require.True(t, ok, "the downloaded blob must be closeable so callers can release its file")
	require.NoError(t, closer.Close())

	entries, err = os.ReadDir(tempFolder)
	require.NoError(t, err)
	require.Empty(t, entries, "closing the blob must remove the downloaded file")
}

func Test_ProcessResourceDigest_LeavesNoTempFile(t *testing.T) {
	tempFolder := t.TempDir()
	repo := NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: tempFolder},
		WithClient(&fakeGetter{body: []byte("digest me")}))

	_, err := repo.ProcessResourceDigest(context.Background(),
		s3Resource(&v1.S3Bucket{BucketName: "b", ObjectKey: "k"}), nil)
	require.NoError(t, err)

	entries, err := os.ReadDir(tempFolder)
	require.NoError(t, err)
	require.Empty(t, entries, "digest processing must not leave the downloaded object behind")
}

func Test_NewResourceRepository_NilFilesystemConfig(t *testing.T) {
	// Redirect the OS temp dir so the downloaded file is cleaned up with it.
	osTempDir := t.TempDir()
	t.Setenv("TMPDIR", osTempDir)

	content := []byte("no config")
	repo := NewResourceRepository(nil, WithClient(&fakeGetter{body: content}))

	b, err := repo.DownloadResource(context.Background(),
		s3Resource(&v1.S3Bucket{BucketName: "b", ObjectKey: "k"}), nil)
	require.NoError(t, err)

	rc, err := b.ReadCloser()
	require.NoError(t, err)
	defer func() { _ = rc.Close() }()
	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.Equal(t, content, got)
}
