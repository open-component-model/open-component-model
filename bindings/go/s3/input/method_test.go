package input

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	credv1 "ocm.software/open-component-model/bindings/go/s3/spec/credentials/v1"
	inputspec "ocm.software/open-component-model/bindings/go/s3/spec/input"
	"ocm.software/open-component-model/bindings/go/s3/spec/input/v2"
)

// fakeS3 is an httptest server answering GetObject with a canned object, so that the
// input method is exercised over the real AWS SDK rather than a stubbed client.
type fakeS3 struct {
	*httptest.Server

	mu        sync.Mutex
	body      []byte
	versionID string
	requests  []s3Request
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
		body, versionID := f.body, f.versionID
		f.mu.Unlock()

		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		if versionID != "" {
			w.Header().Set("X-Amz-Version-Id", versionID)
		}
		// Real S3 reports a checksum, and the SDK warns about every response carrying none.
		sum := make([]byte, 4)
		binary.BigEndian.PutUint32(sum, crc32.ChecksumIEEE(body))
		w.Header().Set("X-Amz-Checksum-Crc32", base64.StdEncoding.EncodeToString(sum))

		_, _ = w.Write(body)
	}))
	t.Cleanup(f.Close)

	return f
}

// serve makes the server answer with body from now on, reporting versionID as the
// object version when it is not empty.
func (f *fakeS3) serve(body []byte, versionID string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.body, f.versionID = body, versionID
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

func s3InputResource(spec *v2.S3) *constructorruntime.Resource {
	spec.Type = inputspec.V2VersionedType
	r := &constructorruntime.Resource{}
	r.Name = "test-resource"
	r.Version = "1.0.0"
	r.Type = "blob"
	r.Input = spec

	return r
}

// servedBy points spec at srv. The fake serves no bucket subdomains, so the input is
// addressed path-style.
func servedBy(srv *fakeS3, spec *v2.S3) *v2.S3 {
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
		id, err := method.GetResourceCredentialConsumerIdentity(t.Context(), s3InputResource(&v2.S3{
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
			s3InputResource(&v2.S3{BucketName: "my-bucket"}))
		require.ErrorContains(t, err, "objectKey is required")

		_, err = method.GetResourceCredentialConsumerIdentity(t.Context(),
			s3InputResource(&v2.S3{ObjectKey: "path/to/blob"}))
		require.ErrorContains(t, err, "bucketName is required")
	})

	t.Run("a resource carrying no input is rejected", func(t *testing.T) {
		_, err := method.GetResourceCredentialConsumerIdentity(t.Context(), &constructorruntime.Resource{})
		require.ErrorContains(t, err, "resource input is required")
	})
}

// read reads b to its end, also when reading fails, and closes it.
func read(t *testing.T, b blob.ReadOnlyBlob) ([]byte, error) {
	t.Helper()
	rc, err := b.ReadCloser()
	if err != nil {
		return nil, err
	}
	got, err := io.ReadAll(rc)
	return got, errors.Join(err, rc.Close())
}

// The input method yields the object as local blob data, which is what makes the
// component version carry the content instead of a reference to the bucket.
func Test_ProcessResource(t *testing.T) {
	content := []byte("hello from s3 input")

	process := func(t *testing.T, method *InputMethod, srv *fakeS3, spec *v2.S3) (blob.ReadOnlyBlob, error) {
		t.Helper()
		result, err := method.ProcessResource(t.Context(), s3InputResource(servedBy(srv, spec)), fakeCredentials())
		if err != nil {
			return nil, err
		}
		require.Nil(t, result.ProcessedResource, "the object is stored as a local blob, not as an access")
		return result.ProcessedBlobData, nil
	}

	files := func(t *testing.T, dir string) []os.DirEntry {
		t.Helper()
		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		return entries
	}

	t.Run("the object is downloaded once and its file removed after the first read", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeS3(t, content, "text/plain")
		dir := t.TempDir()

		b, err := process(t, &InputMethod{TempFolder: dir}, srv, &v2.S3{BucketName: "my-bucket", ObjectKey: "path/blob.txt"})
		r.NoError(err)
		r.Len(srv.recorded(), 1, "the object is downloaded once to describe it")
		r.Len(files(t, dir), 1, "the download is kept for the first read")

		digestAware, ok := b.(blob.DigestAware)
		r.True(ok)
		dig, known := digestAware.Digest()
		r.True(known)
		r.Equal(digest.FromBytes(content).String(), dig)
		sizeAware, ok := b.(blob.SizeAware)
		r.True(ok)
		r.Equal(int64(len(content)), sizeAware.Size())
		mediaTypeAware, ok := b.(blob.MediaTypeAware)
		r.True(ok)
		mediaType, known := mediaTypeAware.MediaType()
		r.True(known)
		r.Equal("text/plain", mediaType)

		got, err := read(t, b)
		r.NoError(err)
		r.Equal(content, got)
		r.Len(srv.recorded(), 1, "the first read is served from the download")
		r.Empty(files(t, dir), "closing the first reader removes the download")

		got, err = read(t, b)
		r.NoError(err)
		r.Equal(content, got)
		requests := srv.recorded()
		r.Len(requests, 2, "later reads request the object again")
		for _, request := range requests {
			r.Equal("/my-bucket/path/blob.txt", request.path)
			r.Empty(request.versionID, "an unpinned input reads the latest version")
		}
	})

	t.Run("an unread download is removed when the context ends", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeS3(t, content, "")
		dir := t.TempDir()
		ctx, cancel := context.WithCancel(t.Context())

		_, err := (&InputMethod{TempFolder: dir}).ProcessResource(ctx, s3InputResource(servedBy(srv, &v2.S3{BucketName: "my-bucket", ObjectKey: "path/blob.txt"})), fakeCredentials())
		r.NoError(err)
		r.Len(files(t, dir), 1)

		cancel()
		r.Eventually(func() bool { return len(files(t, dir)) == 0 }, 5*time.Second, 10*time.Millisecond)
	})

	t.Run("a pinned version is requested", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeS3(t, content, "")

		b, err := process(t, &InputMethod{TempFolder: t.TempDir()}, srv, &v2.S3{BucketName: "my-bucket", ObjectKey: "path/blob.txt", Version: "v-1"})
		r.NoError(err)
		for range 2 {
			_, err = read(t, b)
			r.NoError(err)
		}

		requests := srv.recorded()
		r.Len(requests, 2)
		for _, request := range requests {
			r.Equal("v-1", request.versionID)
		}
	})

	t.Run("later reads are pinned to the version the store reported", func(t *testing.T) {
		for _, tt := range []struct {
			name      string
			versionID string
			want      string
		}{
			{name: "a real version is pinned", versionID: "v-2", want: "v-2"},
			{name: "the unversioned placeholder is not", versionID: "null"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				r := require.New(t)
				srv := newFakeS3(t, content, "")
				srv.serve(content, tt.versionID)

				b, err := process(t, &InputMethod{TempFolder: t.TempDir()}, srv, &v2.S3{BucketName: "my-bucket", ObjectKey: "path/blob.txt"})
				r.NoError(err)
				for range 2 {
					_, err = read(t, b)
					r.NoError(err)
				}

				requests := srv.recorded()
				r.Len(requests, 2)
				r.Empty(requests[0].versionID)
				r.Equal(tt.want, requests[1].versionID)
			})
		}
	})

	t.Run("an object overwritten after it was downloaded fails a later read", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeS3(t, content, "")

		b, err := process(t, &InputMethod{TempFolder: t.TempDir()}, srv, &v2.S3{BucketName: "my-bucket", ObjectKey: "path/blob.txt"})
		r.NoError(err)
		srv.serve([]byte("overwritten content"), "")

		got, err := read(t, b)
		r.NoError(err, "the first read serves the download")
		r.Equal(content, got)
		_, err = read(t, b)
		r.ErrorContains(err, "mismatch")
	})

	t.Run("the spec media type overrides the one the store reports", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeS3(t, content, "text/plain")

		b, err := process(t, &InputMethod{}, srv, &v2.S3{BucketName: "my-bucket", ObjectKey: "path/blob.txt", MediaType: "application/custom"})
		r.NoError(err)

		mediaTypeAware, ok := b.(blob.MediaTypeAware)
		r.True(ok)
		mediaType, known := mediaTypeAware.MediaType()
		r.True(known)
		r.Equal("application/custom", mediaType)
	})

	t.Run("an object exceeding the maximum size is rejected", func(t *testing.T) {
		srv := newFakeS3(t, content, "")
		maxDownloadSize := int64(1)

		_, err := process(t, &InputMethod{MaxDownloadSize: &maxDownloadSize}, srv, &v2.S3{BucketName: "my-bucket", ObjectKey: "path/blob.txt"})
		require.ErrorContains(t, err, "exceeds maximum allowed size")
	})

	t.Run("an invalid input spec is rejected before anything is requested", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeS3(t, content, "")

		_, err := process(t, &InputMethod{}, srv, &v2.S3{BucketName: "my-bucket"})
		r.ErrorContains(err, "objectKey is required")
		r.Empty(srv.recorded())
	})
}

// The scheme has to accept every spelling the constructor may carry, because an input
// is written by hand.
func Test_InputMethodScheme(t *testing.T) {
	method := &InputMethod{}
	scheme := method.GetInputMethodScheme()

	for _, typ := range []runtime.Type{
		runtime.NewVersionedType(v2.Type, v2.Version),
		runtime.NewUnversionedType(v2.Type),
		runtime.NewVersionedType(v2.LowerCamelType, v2.Version),
		runtime.NewUnversionedType(v2.LowerCamelType),
	} {
		t.Run(typ.String(), func(t *testing.T) {
			spec := &v2.S3{}
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
