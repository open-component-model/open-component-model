package transformation_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	godigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	filesystemaccess "ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/wget/repository"
	wgetaccess "ocm.software/open-component-model/bindings/go/wget/spec/access"
	wgetv1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/wget/transformation"
	"ocm.software/open-component-model/bindings/go/wget/transformation/spec/v1alpha1"
)

func newTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	v2.MustAddToScheme(scheme)
	filesystemaccess.MustAddToScheme(scheme)
	wgetaccess.MustAddToScheme(scheme)
	scheme.MustRegisterWithAlias(&v1alpha1.DownloadWgetResource{}, v1alpha1.DownloadWgetResourceV1alpha1)
	return scheme
}

func wgetResource(t *testing.T, url string) *v2.Resource {
	t.Helper()
	access := &wgetv1.Wget{
		Type: runtime.NewVersionedType(wgetaccess.WgetConsumerType, "v1"),
		URL:  url,
	}
	raw := &runtime.Raw{}
	require.NoError(t, wgetaccess.Scheme.Convert(access, raw))
	return &v2.Resource{
		ElementMeta: v2.ElementMeta{
			ObjectMeta: v2.ObjectMeta{
				Name:    "myfile",
				Version: "1.0.0",
			},
		},
		Type:   "blob",
		Access: raw,
	}
}

func TestDownloadWgetResource_Transform(t *testing.T) {
	t.Parallel()

	scheme := newTestScheme()
	const content = "hello wget transfer"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(content))
	}))
	t.Cleanup(srv.Close)

	t.Run("downloads resource to a file", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)
		ctx := t.Context()

		transform := &transformation.DownloadWgetResource{
			Scheme:             scheme,
			ResourceRepository: repository.NewResourceRepository(nil),
		}

		spec := &v1alpha1.DownloadWgetResource{
			Type: v1alpha1.DownloadWgetResourceV1alpha1,
			ID:   "test-get-wget",
			Spec: &v1alpha1.DownloadWgetResourceSpec{
				Resource: wgetResource(t, srv.URL+"/myfile"),
			},
		}

		result, err := transform.Transform(ctx, spec)
		r.NoError(err)
		r.NotNil(result)

		out, ok := result.(*v1alpha1.DownloadWgetResource)
		r.True(ok)
		r.NotNil(out.Output)
		r.NotNil(out.Output.Resource)

		filePath := strings.TrimPrefix(out.Output.File.URI, "file://")
		assert.FileExists(t, filePath)
		t.Cleanup(func() { _ = os.RemoveAll(filePath) })

		data, err := os.ReadFile(filePath)
		r.NoError(err)
		assert.Equal(t, content, string(data), "downloaded content should match served bytes")

		assert.Equal(t, "myfile", out.Output.Resource.Name)
		assert.Equal(t, "1.0.0", out.Output.Resource.Version)
	})

	t.Run("downloads to specified output directory", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)
		ctx := t.Context()
		outputDir := t.TempDir()

		transform := &transformation.DownloadWgetResource{
			Scheme:             scheme,
			ResourceRepository: repository.NewResourceRepository(nil),
		}

		spec := &v1alpha1.DownloadWgetResource{
			Type: v1alpha1.DownloadWgetResourceV1alpha1,
			ID:   "test-get-wget-output-path",
			Spec: &v1alpha1.DownloadWgetResourceSpec{
				Resource:   wgetResource(t, srv.URL+"/myfile"),
				OutputPath: outputDir,
			},
		}

		result, err := transform.Transform(ctx, spec)
		r.NoError(err)

		out, ok := result.(*v1alpha1.DownloadWgetResource)
		r.True(ok)
		r.NotNil(out.Output)

		filePath := strings.TrimPrefix(out.Output.File.URI, "file://")
		assert.FileExists(t, filePath)
		assert.True(t, strings.HasPrefix(filePath, outputDir))
	})

	t.Run("removes the output file when the download fails", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)
		ctx := t.Context()
		outputDir := t.TempDir()

		failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		}))
		t.Cleanup(failSrv.Close)

		transform := &transformation.DownloadWgetResource{
			Scheme:             scheme,
			ResourceRepository: repository.NewResourceRepository(nil),
		}

		spec := &v1alpha1.DownloadWgetResource{
			Type: v1alpha1.DownloadWgetResourceV1alpha1,
			ID:   "test-cleanup-on-failure",
			Spec: &v1alpha1.DownloadWgetResourceSpec{
				Resource:   wgetResource(t, failSrv.URL+"/myfile"),
				OutputPath: outputDir,
			},
		}

		result, err := transform.Transform(ctx, spec)
		r.Error(err)
		r.Nil(result)

		entries, err := os.ReadDir(outputDir)
		r.NoError(err)
		assert.Empty(t, entries, "temporary output file should be removed after a failed download")
	})

	t.Run("removes the output file when verification fails while reading", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)
		ctx := t.Context()
		outputDir := t.TempDir()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("not what was promised"))
		}))
		t.Cleanup(srv.Close)

		// A pinned media type keeps DownloadResource from sending the request, so the
		// blob is lazy and the digest mismatch only fails the read in the copy.
		access := &wgetv1.Wget{
			Type:      runtime.NewVersionedType(wgetaccess.WgetConsumerType, "v1"),
			URL:       srv.URL + "/myfile",
			MediaType: "text/plain",
		}
		raw := &runtime.Raw{}
		r.NoError(wgetaccess.Scheme.Convert(access, raw))
		resource := wgetResource(t, srv.URL+"/myfile")
		resource.Access = raw
		resource.Digest = &v2.Digest{
			HashAlgorithm:          "SHA-256",
			NormalisationAlgorithm: "genericBlobDigest/v1",
			Value:                  godigest.FromString(content).Encoded(),
		}

		transform := &transformation.DownloadWgetResource{
			Scheme:             scheme,
			ResourceRepository: repository.NewResourceRepository(nil, repository.WithHTTPClient(srv.Client())),
		}
		spec := &v1alpha1.DownloadWgetResource{
			Type: v1alpha1.DownloadWgetResourceV1alpha1,
			ID:   "test-cleanup-on-read-failure",
			Spec: &v1alpha1.DownloadWgetResourceSpec{Resource: resource, OutputPath: outputDir},
		}

		result, err := transform.Transform(ctx, spec)
		r.ErrorContains(err, "digest mismatch")
		r.Nil(result)

		entries, err := os.ReadDir(outputDir)
		r.NoError(err)
		assert.Empty(t, entries, "the output file must be removed when the read fails")
	})

	t.Run("fails when spec is nil", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)
		ctx := t.Context()

		transform := &transformation.DownloadWgetResource{
			Scheme:             scheme,
			ResourceRepository: repository.NewResourceRepository(nil),
		}

		spec := &v1alpha1.DownloadWgetResource{
			Type: v1alpha1.DownloadWgetResourceV1alpha1,
			ID:   "test-nil-spec",
			Spec: nil,
		}

		result, err := transform.Transform(ctx, spec)
		r.Error(err)
		r.Nil(result)
		assert.Contains(t, err.Error(), "spec is required")
	})

	t.Run("fails when resource is nil", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)
		ctx := t.Context()

		transform := &transformation.DownloadWgetResource{
			Scheme:             scheme,
			ResourceRepository: repository.NewResourceRepository(nil),
		}

		spec := &v1alpha1.DownloadWgetResource{
			Type: v1alpha1.DownloadWgetResourceV1alpha1,
			ID:   "test-nil-resource",
			Spec: &v1alpha1.DownloadWgetResourceSpec{
				Resource: nil,
			},
		}

		result, err := transform.Transform(ctx, spec)
		r.Error(err)
		r.Nil(result)
		assert.Contains(t, err.Error(), "resource is required")
	})
}
