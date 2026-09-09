package input

import (
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	credv1 "ocm.software/open-component-model/bindings/go/s3/spec/credentials/v1"
	inputspec "ocm.software/open-component-model/bindings/go/s3/spec/input"
	v1 "ocm.software/open-component-model/bindings/go/s3/spec/input/v1"
)

// fakeS3 is an httptest server answering GetObject with a canned object, so that the
// input method is exercised over the real AWS SDK rather than a stubbed client.
type fakeS3 struct {
	*httptest.Server

	body []byte

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

// newFakeS3 starts a server serving body with contentType, and stops it when the test
// ends.
func newFakeS3(t *testing.T, body []byte, contentType string) *fakeS3 {
	t.Helper()

	f := &fakeS3{body: body}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, s3Request{
			path:      r.URL.Path,
			versionID: r.URL.Query().Get("versionId"),
		})
		f.mu.Unlock()

		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
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

func s3InputResource(spec *v1.S3Bucket) *constructorruntime.Resource {
	spec.Type = inputspec.V1VersionedType
	r := &constructorruntime.Resource{}
	r.Name = "test-resource"
	r.Version = "1.0.0"
	r.Type = "blob"
	r.Input = spec

	return r
}

// servedBy points spec at srv. The fake serves no bucket subdomains, so the input is
// addressed path-style.
func servedBy(srv *fakeS3, spec *v1.S3Bucket) *v1.S3Bucket {
	spec.Endpoint = srv.URL
	spec.UsePathStyle = true

	return spec
}

// The identity itself is built by [identityv1.IdentityFromObject] and covered by its own
// tests. What the input method adds is carrying the input spec into it, and rejecting an
// input that names no object before it gets there.
func Test_GetResourceCredentialConsumerIdentity(t *testing.T) {
	method := &InputMethod{}

	t.Run("the input spec is carried into the identity", func(t *testing.T) {
		id, err := method.GetResourceCredentialConsumerIdentity(t.Context(), s3InputResource(&v1.S3Bucket{
			BucketName: "my-bucket",
			ObjectKey:  "path/to/blob",
			Endpoint:   "https://minio.example.com:9000",
		}))
		require.NoError(t, err)
		require.Equal(t, "my-bucket/path/to/blob", id[runtime.IdentityAttributePath])
		require.Equal(t, "minio.example.com", id[runtime.IdentityAttributeHostname])
		require.Equal(t, "9000", id[runtime.IdentityAttributePort])
	})

	t.Run("an input naming no object is rejected", func(t *testing.T) {
		_, err := method.GetResourceCredentialConsumerIdentity(t.Context(),
			s3InputResource(&v1.S3Bucket{BucketName: "my-bucket"}))
		require.ErrorContains(t, err, "objectKey is required")

		_, err = method.GetResourceCredentialConsumerIdentity(t.Context(),
			s3InputResource(&v1.S3Bucket{ObjectKey: "path/to/blob"}))
		require.ErrorContains(t, err, "bucketName is required")
	})

	t.Run("a resource carrying no input is rejected", func(t *testing.T) {
		_, err := method.GetResourceCredentialConsumerIdentity(t.Context(), &constructorruntime.Resource{})
		require.ErrorContains(t, err, "resource input is required")
	})
}

// The input method yields the object as local blob data, which is what makes the
// component version carry the content instead of a reference to the bucket.
func Test_ProcessResource(t *testing.T) {
	content := []byte("hello from s3 input")

	t.Run("the object is returned as local blob data", func(t *testing.T) {
		srv := newFakeS3(t, content, "text/plain")
		method := &InputMethod{TempFolder: t.TempDir()}

		result, err := method.ProcessResource(t.Context(), s3InputResource(servedBy(srv, &v1.S3Bucket{
			BucketName: "my-bucket",
			ObjectKey:  "path/blob.txt",
		})), fakeCredentials())
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Nil(t, result.ProcessedResource, "the object is stored as a local blob, not as an access")
		require.NotNil(t, result.ProcessedBlobData)

		rc, err := result.ProcessedBlobData.ReadCloser()
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, rc.Close()) })
		got, err := io.ReadAll(rc)
		require.NoError(t, err)
		require.Equal(t, content, got)

		mediaTypeAware, ok := result.ProcessedBlobData.(blob.MediaTypeAware)
		require.True(t, ok)
		mediaType, known := mediaTypeAware.MediaType()
		require.True(t, known)
		require.Equal(t, "text/plain", mediaType)

		requests := srv.recorded()
		require.Len(t, requests, 1)
		require.Equal(t, "/my-bucket/path/blob.txt", requests[0].path)
		require.Empty(t, requests[0].versionID, "an unpinned input reads the latest version")
	})

	t.Run("a pinned version is requested", func(t *testing.T) {
		srv := newFakeS3(t, content, "")
		method := &InputMethod{TempFolder: t.TempDir()}

		_, err := method.ProcessResource(t.Context(), s3InputResource(servedBy(srv, &v1.S3Bucket{
			BucketName: "my-bucket",
			ObjectKey:  "path/blob.txt",
			Version:    "v-1",
		})), fakeCredentials())
		require.NoError(t, err)

		requests := srv.recorded()
		require.Len(t, requests, 1)
		require.Equal(t, "v-1", requests[0].versionID)
	})

	t.Run("the spec media type overrides the one the store reports", func(t *testing.T) {
		srv := newFakeS3(t, content, "text/plain")
		method := &InputMethod{TempFolder: t.TempDir()}

		result, err := method.ProcessResource(t.Context(), s3InputResource(servedBy(srv, &v1.S3Bucket{
			BucketName: "my-bucket",
			ObjectKey:  "path/blob.txt",
			MediaType:  "application/custom",
		})), fakeCredentials())
		require.NoError(t, err)

		mediaTypeAware, ok := result.ProcessedBlobData.(blob.MediaTypeAware)
		require.True(t, ok)
		mediaType, known := mediaTypeAware.MediaType()
		require.True(t, known)
		require.Equal(t, "application/custom", mediaType)
	})

	t.Run("an object exceeding the maximum size is rejected", func(t *testing.T) {
		srv := newFakeS3(t, content, "")
		maxDownloadSize := int64(1)
		method := &InputMethod{TempFolder: t.TempDir(), MaxDownloadSize: &maxDownloadSize}

		_, err := method.ProcessResource(t.Context(), s3InputResource(servedBy(srv, &v1.S3Bucket{
			BucketName: "my-bucket",
			ObjectKey:  "path/blob.txt",
		})), fakeCredentials())
		require.ErrorContains(t, err, "exceeds maximum allowed size")
	})

	t.Run("an invalid input spec is rejected before anything is requested", func(t *testing.T) {
		srv := newFakeS3(t, content, "")
		method := &InputMethod{TempFolder: t.TempDir()}

		_, err := method.ProcessResource(t.Context(),
			s3InputResource(servedBy(srv, &v1.S3Bucket{BucketName: "my-bucket"})), fakeCredentials())
		require.ErrorContains(t, err, "objectKey is required")
		require.Empty(t, srv.recorded())
	})
}

// The scheme has to accept every spelling the constructor may carry, because an input
// is written by hand.
func Test_InputMethodScheme(t *testing.T) {
	method := &InputMethod{}
	scheme := method.GetInputMethodScheme()

	for _, typ := range []runtime.Type{
		runtime.NewVersionedType(v1.Type, v1.Version),
		runtime.NewUnversionedType(v1.Type),
		runtime.NewVersionedType(v1.LowerCamelType, v1.Version),
		runtime.NewUnversionedType(v1.LowerCamelType),
	} {
		t.Run(typ.String(), func(t *testing.T) {
			spec := &v1.S3Bucket{}
			raw := &runtime.Raw{
				Type: typ,
				Data: []byte(`{"type":"` + typ.String() + `","bucketName":"my-bucket","objectKey":"path/blob.txt"}`),
			}
			require.NoError(t, scheme.Convert(raw, spec))
			require.Equal(t, "my-bucket", spec.BucketName)
			require.Equal(t, "path/blob.txt", spec.ObjectKey)
		})
	}
}
