package transformation_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

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
	scheme.MustRegisterWithAlias(&v1alpha1.GetWget{}, v1alpha1.GetWgetV1alpha1)
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

func TestGetWget_Transform(t *testing.T) {
	t.Parallel()

	scheme := newTestScheme()
	const content = "hello wget transfer"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(content))
	}))
	t.Cleanup(srv.Close)

	t.Run("downloads resource to a file", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()

		transform := &transformation.GetWget{
			Scheme:             scheme,
			ResourceRepository: repository.NewResourceRepository(),
		}

		spec := &v1alpha1.GetWget{
			Type: v1alpha1.GetWgetV1alpha1,
			ID:   "test-get-wget",
			Spec: &v1alpha1.GetWgetSpec{
				Resource: wgetResource(t, srv.URL+"/myfile"),
			},
		}

		result, err := transform.Transform(ctx, spec)
		r.NoError(err)
		r.NotNil(result)

		out, ok := result.(*v1alpha1.GetWget)
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
		r := require.New(t)
		ctx := t.Context()
		outputDir := t.TempDir()

		transform := &transformation.GetWget{
			Scheme:             scheme,
			ResourceRepository: repository.NewResourceRepository(),
		}

		spec := &v1alpha1.GetWget{
			Type: v1alpha1.GetWgetV1alpha1,
			ID:   "test-get-wget-output-path",
			Spec: &v1alpha1.GetWgetSpec{
				Resource:   wgetResource(t, srv.URL+"/myfile"),
				OutputPath: outputDir,
			},
		}

		result, err := transform.Transform(ctx, spec)
		r.NoError(err)

		out, ok := result.(*v1alpha1.GetWget)
		r.True(ok)
		r.NotNil(out.Output)

		filePath := strings.TrimPrefix(out.Output.File.URI, "file://")
		assert.FileExists(t, filePath)
		assert.True(t, strings.HasPrefix(filePath, outputDir))
	})

	t.Run("fails when spec is nil", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()

		transform := &transformation.GetWget{
			Scheme:             scheme,
			ResourceRepository: repository.NewResourceRepository(),
		}

		spec := &v1alpha1.GetWget{
			Type: v1alpha1.GetWgetV1alpha1,
			ID:   "test-nil-spec",
			Spec: nil,
		}

		result, err := transform.Transform(ctx, spec)
		r.Error(err)
		r.Nil(result)
		assert.Contains(t, err.Error(), "spec is required")
	})

	t.Run("fails when resource is nil", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()

		transform := &transformation.GetWget{
			Scheme:             scheme,
			ResourceRepository: repository.NewResourceRepository(),
		}

		spec := &v1alpha1.GetWget{
			Type: v1alpha1.GetWgetV1alpha1,
			ID:   "test-nil-resource",
			Spec: &v1alpha1.GetWgetSpec{
				Resource: nil,
			},
		}

		result, err := transform.Transform(ctx, spec)
		r.Error(err)
		r.Nil(result)
		assert.Contains(t, err.Error(), "resource is required")
	})
}
