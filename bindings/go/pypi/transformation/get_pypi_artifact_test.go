package transformation

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/credentials"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/pypi/repository/resource"
	"ocm.software/open-component-model/bindings/go/pypi/spec/access"
	"ocm.software/open-component-model/bindings/go/pypi/spec/access/v1alpha1"
	credsv1 "ocm.software/open-component-model/bindings/go/pypi/spec/credentials/v1"
	pypiv1alpha1 "ocm.software/open-component-model/bindings/go/pypi/transformation/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	wheelName = "requests-2.32.3-py3-none-any.whl"
	sdistName = "requests-2.32.3.tar.gz"
)

// mockPyPIIndex serves the project page (JSON) plus the wheel (+ its .asc) and
// the sdist, and records the Authorization header it last saw.
func mockPyPIIndex(t *testing.T) (indexURL string, lastAuth *string) {
	t.Helper()
	var auth string
	page := `{"meta":{"api-version":"1.1"},"name":"requests","files":[
	  {"filename":"` + wheelName + `","url":"/files/` + wheelName + `","hashes":{"sha256":"abc"},"gpg-sig":true},
	  {"filename":"` + sdistName + `","url":"/files/` + sdistName + `","hashes":{"sha256":"def"}}
	]}`
	files := map[string]string{
		"/files/" + wheelName:          "WHEELDATA",
		"/files/" + wheelName + ".asc": "SIGDATA",
		"/files/" + sdistName:          "SDISTDATA",
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		if r.URL.Path == "/simple/requests/" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(page))
			return
		}
		if data, ok := files[r.URL.Path]; ok {
			_, _ = w.Write([]byte(data))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	return server.URL + "/simple", &auth
}

func pypiV2Resource(t *testing.T, indexURL, version string, dists ...v1alpha1.Distribution) *v2.Resource {
	t.Helper()
	pypiAccess := &v1alpha1.PyPI{
		Type:          runtime.NewVersionedType(v1alpha1.Type, v1alpha1.Version),
		IndexURL:      indexURL,
		Project:       "requests",
		Version:       version,
		Distributions: dists,
	}
	raw := runtime.Raw{}
	require.NoError(t, access.Scheme.Convert(pypiAccess, &raw))
	return &v2.Resource{
		ElementMeta: v2.ElementMeta{ObjectMeta: v2.ObjectMeta{Name: "requests", Version: version}},
		Type:        "pythonPackage",
		Access:      &raw,
	}
}

func tgzNames(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()
	gz, err := gzip.NewReader(f)
	require.NoError(t, err)
	tr := tar.NewReader(gz)
	var names []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		names = append(names, h.Name)
	}
	return names
}

func TestGetPyPIArtifact_Transform(t *testing.T) {
	indexURL, _ := mockPyPIIndex(t)

	transformer := &GetPyPIArtifact{
		Scheme:             pypiv1alpha1.Scheme,
		ResourceRepository: resource.NewResourceRepository(),
	}

	outDir := t.TempDir()
	step := &pypiv1alpha1.GetPyPIArtifact{
		Type: pypiv1alpha1.GetPyPIArtifactV1alpha1,
		ID:   "get-pypi-artifact",
		Spec: &pypiv1alpha1.GetPyPIArtifactSpec{
			Resource:   pypiV2Resource(t, indexURL, "2.32.3"),
			OutputPath: outDir,
		},
	}

	result, err := transformer.Transform(t.Context(), step)
	require.NoError(t, err)

	var transformed pypiv1alpha1.GetPyPIArtifact
	require.NoError(t, pypiv1alpha1.Scheme.Convert(result, &transformed))
	require.NotNil(t, transformed.Output)

	require.NotEmpty(t, transformed.Output.File.URI)
	contentPath := strings.TrimPrefix(transformed.Output.File.URI, "file://")
	t.Cleanup(func() { _ = os.Remove(contentPath) })
	assert.Equal(t, outDir, filepath.Dir(contentPath), "the archive must land in the spec's outputPath")
	assert.Equal(t, "application/x-tgz", transformed.Output.File.MediaType)

	assert.Equal(t, []string{wheelName, wheelName + ".asc", sdistName}, tgzNames(t, contentPath),
		"the archive must hold the files in deterministic order with the signature after its file")

	assert.Equal(t, step.Spec.Resource, transformed.Output.Resource)
}

func TestGetPyPIArtifact_Transform_RemovesOutputOnFailure(t *testing.T) {
	indexURL, _ := mockPyPIIndex(t)
	outDir := t.TempDir()

	transformer := &GetPyPIArtifact{
		Scheme:             pypiv1alpha1.Scheme,
		ResourceRepository: resource.NewResourceRepository(),
	}

	// The mock only serves 2.32.3, so any other version resolves no files.
	_, err := transformer.Transform(t.Context(), &pypiv1alpha1.GetPyPIArtifact{
		Type: pypiv1alpha1.GetPyPIArtifactV1alpha1,
		ID:   "get-pypi-artifact",
		Spec: &pypiv1alpha1.GetPyPIArtifactSpec{
			Resource:   pypiV2Resource(t, indexURL, "9.9.9"),
			OutputPath: outDir,
		},
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "error downloading pypi artifact")

	entries, err := os.ReadDir(outDir)
	require.NoError(t, err)
	assert.Empty(t, entries, "a failed download must not leave the output file behind")
}

type stubResolver struct {
	typed runtime.Typed
	err   error
	calls int
}

func (s *stubResolver) Resolve(_ context.Context, _ runtime.Identity) (runtime.Typed, error) {
	s.calls++
	return s.typed, s.err
}

func TestGetPyPIArtifact_Transform_ResolveCredentials(t *testing.T) {
	step := func(indexURL string) *pypiv1alpha1.GetPyPIArtifact {
		return &pypiv1alpha1.GetPyPIArtifact{
			Type: pypiv1alpha1.GetPyPIArtifactV1alpha1,
			ID:   "get-pypi-artifact",
			Spec: &pypiv1alpha1.GetPyPIArtifactSpec{Resource: pypiV2Resource(t, indexURL, "2.32.3")},
		}
	}
	cleanup := func(t *testing.T, result runtime.Typed) {
		var transformed pypiv1alpha1.GetPyPIArtifact
		require.NoError(t, pypiv1alpha1.Scheme.Convert(result, &transformed))
		t.Cleanup(func() { _ = os.Remove(strings.TrimPrefix(transformed.Output.File.URI, "file://")) })
	}

	t.Run("ErrNotFound means anonymous", func(t *testing.T) {
		indexURL, lastAuth := mockPyPIIndex(t)
		resolver := &stubResolver{err: credentials.ErrNotFound}
		transformer := &GetPyPIArtifact{Scheme: pypiv1alpha1.Scheme, ResourceRepository: resource.NewResourceRepository(), CredentialProvider: resolver}
		result, err := transformer.Transform(t.Context(), step(indexURL))
		require.NoError(t, err)
		require.Equal(t, 1, resolver.calls)
		assert.Empty(t, *lastAuth)
		cleanup(t, result)
	})

	t.Run("other resolver errors fail the transform", func(t *testing.T) {
		indexURL, _ := mockPyPIIndex(t)
		transformer := &GetPyPIArtifact{Scheme: pypiv1alpha1.Scheme, ResourceRepository: resource.NewResourceRepository(), CredentialProvider: &stubResolver{err: assert.AnError}}
		_, err := transformer.Transform(t.Context(), step(indexURL))
		require.ErrorIs(t, err, assert.AnError)
	})

	t.Run("resolved credentials reach the download", func(t *testing.T) {
		indexURL, lastAuth := mockPyPIIndex(t)
		transformer := &GetPyPIArtifact{Scheme: pypiv1alpha1.Scheme, ResourceRepository: resource.NewResourceRepository(), CredentialProvider: &stubResolver{typed: &credsv1.PyPICredentials{
			Type:          runtime.NewVersionedType(credsv1.PyPICredentialsType, credsv1.Version),
			IdentityToken: "test-token",
		}}}
		result, err := transformer.Transform(t.Context(), step(indexURL))
		require.NoError(t, err)
		assert.Equal(t, "Bearer test-token", *lastAuth)
		cleanup(t, result)
	})
}

func TestGetPyPIArtifact_Transform_RequiresSpec(t *testing.T) {
	transformer := &GetPyPIArtifact{Scheme: pypiv1alpha1.Scheme, ResourceRepository: resource.NewResourceRepository()}
	for name, tc := range map[string]struct {
		spec    *pypiv1alpha1.GetPyPIArtifactSpec
		wantErr string
	}{
		"nil spec":     {spec: nil, wantErr: "spec is required for get pypi artifact transformation"},
		"nil resource": {spec: &pypiv1alpha1.GetPyPIArtifactSpec{}, wantErr: "resource is required in spec for get pypi artifact transformation"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := transformer.Transform(t.Context(), &pypiv1alpha1.GetPyPIArtifact{Type: pypiv1alpha1.GetPyPIArtifactV1alpha1, Spec: tc.spec})
			assert.ErrorContains(t, err, tc.wantErr)
		})
	}
}
