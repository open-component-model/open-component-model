package pypi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/pypi/spec/access/v1alpha1"
)

const (
	wheelName  = "requests-2.32.3-py3-none-any.whl"
	sdistName  = "requests-2.32.3.tar.gz"
	oldWheel   = "requests-2.31.0-py3-none-any.whl"
	yankedName = "requests-2.32.3-99-py3-none-any.whl"
)

// jsonBody is a PEP 691 project detail page for requests with a 2.32.3 wheel
// (with gpg-sig) and sdist, an older 2.31.0 wheel, and a yanked 2.32.3 wheel
// (build tag 99).
const jsonBody = `{
  "meta": {"api-version": "1.1"},
  "name": "requests",
  "files": [
    {"filename": "requests-2.32.3-py3-none-any.whl", "url": "files/requests-2.32.3-py3-none-any.whl", "hashes": {"sha256": "abc"}, "gpg-sig": true},
    {"filename": "requests-2.32.3.tar.gz", "url": "https://cdn.example/requests-2.32.3.tar.gz", "hashes": {"sha256": "def"}},
    {"filename": "requests-2.31.0-py3-none-any.whl", "url": "files/requests-2.31.0-py3-none-any.whl", "hashes": {"sha256": "old"}},
    {"filename": "requests-2.32.3-99-py3-none-any.whl", "url": "files/requests-2.32.3-99-py3-none-any.whl", "hashes": {}, "yanked": "broken"}
  ]
}`

// htmlBody is the PEP 503 equivalent of jsonBody.
const htmlBody = `<!DOCTYPE html><html><body>
<a href="files/requests-2.32.3-py3-none-any.whl#sha256=abc" data-gpg-sig="true">requests-2.32.3-py3-none-any.whl</a>
<a href="https://cdn.example/requests-2.32.3.tar.gz#sha256=def">requests-2.32.3.tar.gz</a>
<a href="files/requests-2.31.0-py3-none-any.whl#sha256=old">requests-2.31.0-py3-none-any.whl</a>
<a href="files/requests-2.32.3-99-py3-none-any.whl" data-yanked="broken">requests-2.32.3-99-py3-none-any.whl</a>
</body></html>`

// indexServer serves the given body with the given content type at
// /simple/requests/, and 404 elsewhere.
func indexServer(t *testing.T, contentType, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/simple/requests/" {
			w.Header().Set("Content-Type", contentType)
			_, _ = w.Write([]byte(body))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func pypiSpec(srv *httptest.Server, project, version string, dists ...v1alpha1.Distribution) *v1alpha1.PyPI {
	return &v1alpha1.PyPI{
		IndexURL:      srv.URL + "/simple",
		Project:       project,
		Version:       version,
		Distributions: dists,
	}
}

func filenames(refs []FileRef) []string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		out = append(out, r.Filename)
	}
	return out
}

func TestResolve_JSON_AllFilesOfVersion(t *testing.T) {
	srv := indexServer(t, "application/vnd.pypi.simple.v1+json", jsonBody)
	refs, err := NewClient(srv.Client()).Resolve(context.Background(), pypiSpec(srv, "requests", "2.32.3"), nil)
	require.NoError(t, err)
	// yanked zip and the 2.31.0 wheel are excluded; sorted by filename.
	assert.Equal(t, []string{wheelName, sdistName}, filenames(refs))
	// wheel keeps its inline hash, gpg-sig, and the sdist absolute url.
	byName := map[string]FileRef{}
	for _, r := range refs {
		byName[r.Filename] = r
	}
	assert.Equal(t, "abc", byName[wheelName].Hashes["sha256"])
	assert.True(t, byName[wheelName].HasGPGSig)
	assert.Equal(t, srv.URL+"/simple/requests/files/requests-2.32.3-py3-none-any.whl", byName[wheelName].URL, "relative url resolved against detail page")
	assert.Equal(t, "https://cdn.example/requests-2.32.3.tar.gz", byName[sdistName].URL, "absolute url preserved")
}

func TestResolve_HTML_AllFilesOfVersion(t *testing.T) {
	srv := indexServer(t, "text/html", htmlBody)
	refs, err := NewClient(srv.Client()).Resolve(context.Background(), pypiSpec(srv, "requests", "2.32.3"), nil)
	require.NoError(t, err)
	assert.Equal(t, []string{wheelName, sdistName}, filenames(refs))
	byName := map[string]FileRef{}
	for _, r := range refs {
		byName[r.Filename] = r
	}
	assert.Equal(t, "abc", byName[wheelName].Hashes["sha256"], "hash taken from URL fragment")
	assert.True(t, byName[wheelName].HasGPGSig)
}

func TestResolve_ProjectNameNormalized(t *testing.T) {
	srv := indexServer(t, "application/vnd.pypi.simple.v1+json", jsonBody)
	// Un-normalized project name still resolves to /simple/requests/.
	refs, err := NewClient(srv.Client()).Resolve(context.Background(), pypiSpec(srv, "Requests", "2.32.3"), nil)
	require.NoError(t, err)
	assert.Equal(t, []string{wheelName, sdistName}, filenames(refs))
}

func TestResolve_KindFilter(t *testing.T) {
	srv := indexServer(t, "application/vnd.pypi.simple.v1+json", jsonBody)
	c := NewClient(srv.Client())
	t.Run("wheel only", func(t *testing.T) {
		refs, err := c.Resolve(context.Background(), pypiSpec(srv, "requests", "2.32.3", v1alpha1.Distribution{Kind: v1alpha1.KindWheel}), nil)
		require.NoError(t, err)
		assert.Equal(t, []string{wheelName}, filenames(refs))
	})
	t.Run("sdist only", func(t *testing.T) {
		refs, err := c.Resolve(context.Background(), pypiSpec(srv, "requests", "2.32.3", v1alpha1.Distribution{Kind: v1alpha1.KindSdist}), nil)
		require.NoError(t, err)
		assert.Equal(t, []string{sdistName}, filenames(refs))
	})
}

func TestResolve_ExactFilename(t *testing.T) {
	srv := indexServer(t, "application/vnd.pypi.simple.v1+json", jsonBody)
	refs, err := NewClient(srv.Client()).Resolve(context.Background(), pypiSpec(srv, "requests", "2.32.3",
		v1alpha1.Distribution{Filename: wheelName}), nil)
	require.NoError(t, err)
	assert.Equal(t, []string{wheelName}, filenames(refs))
}

func TestResolve_ExactFilename_AllowsYanked(t *testing.T) {
	srv := indexServer(t, "application/vnd.pypi.simple.v1+json", jsonBody)
	refs, err := NewClient(srv.Client()).Resolve(context.Background(), pypiSpec(srv, "requests", "2.32.3",
		v1alpha1.Distribution{Filename: "requests-2.32.3-99-py3-none-any.whl"}), nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"requests-2.32.3-99-py3-none-any.whl"}, filenames(refs), "an explicitly named yanked file is still selected")
}

func TestResolve_Errors(t *testing.T) {
	srv := indexServer(t, "application/vnd.pypi.simple.v1+json", jsonBody)
	c := NewClient(srv.Client())
	t.Run("no files for version", func(t *testing.T) {
		_, err := c.Resolve(context.Background(), pypiSpec(srv, "requests", "9.9.9"), nil)
		require.ErrorContains(t, err, "no files found")
	})
	t.Run("named file absent", func(t *testing.T) {
		_, err := c.Resolve(context.Background(), pypiSpec(srv, "requests", "2.32.3", v1alpha1.Distribution{Filename: "nope.whl"}), nil)
		require.ErrorContains(t, err, "not found")
	})
	t.Run("kind absent for version", func(t *testing.T) {
		// 2.31.0 has only a wheel, so requesting sdist yields nothing.
		_, err := c.Resolve(context.Background(), pypiSpec(srv, "requests", "2.31.0", v1alpha1.Distribution{Kind: v1alpha1.KindSdist}), nil)
		require.ErrorContains(t, err, "no sdist files")
	})
	t.Run("project not found", func(t *testing.T) {
		_, err := c.Resolve(context.Background(), pypiSpec(srv, "absent", "1.0"), nil)
		require.ErrorContains(t, err, "not found")
	})
	t.Run("index server error", func(t *testing.T) {
		bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}))
		t.Cleanup(bad.Close)
		_, err := c.Resolve(context.Background(), pypiSpec(&httptest.Server{URL: bad.URL}, "requests", "2.32.3"), nil)
		require.ErrorContains(t, err, "403")
	})
}

func TestResolve_InvalidJSON(t *testing.T) {
	srv := indexServer(t, "application/vnd.pypi.simple.v1+json", "{not json")
	_, err := NewClient(srv.Client()).Resolve(context.Background(), pypiSpec(srv, "requests", "2.32.3"), nil)
	require.ErrorContains(t, err, "invalid JSON")
}

func TestProjectDetailURL(t *testing.T) {
	for _, tc := range []struct {
		index, project, want string
	}{
		{"https://pypi.org/simple", "requests", "https://pypi.org/simple/requests/"},
		{"https://pypi.org/simple/", "Requests", "https://pypi.org/simple/requests/"},
		{"https://pypi.org/simple", "Foo.Bar_Baz", "https://pypi.org/simple/foo-bar-baz/"},
	} {
		got, err := ProjectDetailURL(tc.index, tc.project)
		require.NoError(t, err)
		assert.Equal(t, tc.want, got)
	}
}

func TestVersionFromFilename(t *testing.T) {
	for name, want := range map[string]struct {
		version string
		ok      bool
	}{
		"requests-2.32.3-py3-none-any.whl":       {"2.32.3", true},
		"numpy-1.26.4-cp312-cp312-win_amd64.whl": {"1.26.4", true},
		"requests-2.32.3.tar.gz":                 {"2.32.3", true},
		"my-lib-1.0.0.tar.gz":                    {"1.0.0", true},
		"pkg-3.1.zip":                            {"3.1", true},
		"README.txt":                             {"", false},
		"nodashwhl.whl":                          {"", false},
	} {
		version, ok := versionFromFilename(name)
		assert.Equal(t, want.ok, ok, name)
		if want.ok {
			assert.Equal(t, want.version, version, name)
		}
	}
}

func TestFileKind(t *testing.T) {
	assert.Equal(t, v1alpha1.KindWheel, fileKind("x-1.0-py3-none-any.whl"))
	assert.Equal(t, v1alpha1.KindSdist, fileKind("x-1.0.tar.gz"))
	assert.Equal(t, v1alpha1.KindSdist, fileKind("x-1.0.zip"))
	assert.Equal(t, "", fileKind("x-1.0.exe"))
}
