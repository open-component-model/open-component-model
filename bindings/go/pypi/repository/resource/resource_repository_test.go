package resource_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	credv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/pypi/repository/resource"
	pypiaccess "ocm.software/open-component-model/bindings/go/pypi/spec/access"
	"ocm.software/open-component-model/bindings/go/pypi/spec/access/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	wheelName = "requests-2.32.3-py3-none-any.whl"
	sdistName = "requests-2.32.3.tar.gz"
)

func pypiResource(indexURL, version string, dists ...v1alpha1.Distribution) *descriptor.Resource {
	return &descriptor.Resource{Access: &v1alpha1.PyPI{
		Type:          runtime.NewVersionedType(v1alpha1.Type, v1alpha1.Version),
		IndexURL:      indexURL,
		Project:       "requests",
		Version:       version,
		Distributions: dists,
	}}
}

func mediaTypeOf(t *testing.T, b blob.ReadOnlyBlob) string {
	t.Helper()
	mt, ok := b.(blob.MediaTypeAware).MediaType()
	require.True(t, ok)
	return mt
}

// jsonIndex serves a PEP 691 project page plus the files it lists. sig controls
// whether the wheel's .asc is served (and with what status).
func jsonIndex(t *testing.T, serveSig bool) (*httptest.Server, map[string][]byte) {
	t.Helper()
	files := map[string][]byte{
		"/files/" + wheelName: []byte("WHEELDATA"),
		"/files/" + sdistName: []byte("SDISTDATA"),
	}
	if serveSig {
		files["/files/"+wheelName+".asc"] = []byte("SIGDATA")
	}
	page := `{"meta":{"api-version":"1.1"},"name":"requests","files":[
	  {"filename":"` + wheelName + `","url":"/files/` + wheelName + `","hashes":{"sha256":"abc"},"gpg-sig":` + boolStr(serveSig) + `},
	  {"filename":"` + sdistName + `","url":"/files/` + sdistName + `","hashes":{"sha256":"def"}}
	]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/simple/requests/" {
			w.Header().Set("Content-Type", "application/vnd.pypi.simple.v1+json")
			_, _ = w.Write([]byte(page))
			return
		}
		if data, ok := files[r.URL.Path]; ok {
			_, _ = w.Write(data)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, files
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func TestGetResourceRepositoryScheme(t *testing.T) {
	assert.Same(t, pypiaccess.Scheme, resource.NewResourceRepository().GetResourceRepositoryScheme())
}

func TestGetResourceCredentialConsumerIdentity(t *testing.T) {
	id, err := resource.NewResourceRepository().GetResourceCredentialConsumerIdentity(context.Background(), pypiResource("https://pypi.org/simple", "2.32.3"))
	require.NoError(t, err)
	assert.Equal(t, "PyPIRepository", id[runtime.IdentityAttributeType])
	assert.Equal(t, "pypi.org", id[runtime.IdentityAttributeHostname])
}

func TestConvertAccess(t *testing.T) {
	r := resource.NewResourceRepository()
	t.Run("nil resource", func(t *testing.T) {
		_, err := r.GetResourceCredentialConsumerIdentity(context.Background(), nil)
		require.ErrorContains(t, err, "access is required")
	})
	t.Run("invalid spec rejected before any request", func(t *testing.T) {
		res := pypiResource("https://pypi.org/simple", "")
		_, err := r.DownloadResource(context.Background(), res, nil)
		require.ErrorContains(t, err, "invalid pypi access")
		require.ErrorContains(t, err, "version")
	})
}

func TestDownloadResource_JSON(t *testing.T) {
	srv, _ := jsonIndex(t, true)
	r := resource.NewResourceRepository(resource.WithHTTPClient(srv.Client()))

	t.Run("all files with signature, sorted, one tgz", func(t *testing.T) {
		b, err := r.DownloadResource(context.Background(), pypiResource(srv.URL+"/simple", "2.32.3"), nil)
		require.NoError(t, err)
		assert.Equal(t, resource.MediaTypeTGZ, mediaTypeOf(t, b))
		// wheel sorts before sdist ("-" < "."); the .asc follows its file.
		assert.Equal(t, []string{wheelName, wheelName + ".asc", sdistName}, readTgzNames(t, b))
		entries := readTgzEntries(t, b)
		assert.Equal(t, []byte("WHEELDATA"), entries[wheelName])
		assert.Equal(t, []byte("SIGDATA"), entries[wheelName+".asc"])
		assert.Equal(t, []byte("SDISTDATA"), entries[sdistName])
	})

	t.Run("wheel kind only", func(t *testing.T) {
		b, err := r.DownloadResource(context.Background(), pypiResource(srv.URL+"/simple", "2.32.3", v1alpha1.Distribution{Kind: v1alpha1.KindWheel}), nil)
		require.NoError(t, err)
		assert.Equal(t, []string{wheelName, wheelName + ".asc"}, readTgzNames(t, b))
	})
}

func TestDownloadResource_HTML(t *testing.T) {
	page := `<!DOCTYPE html><html><body>
	<a href="/files/` + wheelName + `#sha256=abc" data-gpg-sig="true">` + wheelName + `</a>
	<a href="/files/` + sdistName + `#sha256=def">` + sdistName + `</a>
	</body></html>`
	files := map[string][]byte{
		"/files/" + wheelName:          []byte("WHEELDATA"),
		"/files/" + wheelName + ".asc": []byte("SIGDATA"),
		"/files/" + sdistName:          []byte("SDISTDATA"),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/simple/requests/" {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(page))
			return
		}
		if data, ok := files[r.URL.Path]; ok {
			_, _ = w.Write(data)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	r := resource.NewResourceRepository(resource.WithHTTPClient(srv.Client()))
	b, err := r.DownloadResource(context.Background(), pypiResource(srv.URL+"/simple", "2.32.3"), nil)
	require.NoError(t, err)
	assert.Equal(t, []string{wheelName, wheelName + ".asc", sdistName}, readTgzNames(t, b),
		"HTML index resolves to the same archive as the JSON index")
}

func TestDownloadResource_NoSignature(t *testing.T) {
	srv, _ := jsonIndex(t, false)
	r := resource.NewResourceRepository(resource.WithHTTPClient(srv.Client()))
	b, err := r.DownloadResource(context.Background(), pypiResource(srv.URL+"/simple", "2.32.3"), nil)
	require.NoError(t, err)
	assert.Equal(t, []string{wheelName, sdistName}, readTgzNames(t, b), "a 404 signature is simply absent")
}

func TestDownloadResource_MissingFileErrors(t *testing.T) {
	// index lists a file the server does not actually serve.
	page := `{"meta":{"api-version":"1.1"},"name":"requests","files":[
	  {"filename":"` + wheelName + `","url":"/files/missing.whl","hashes":{}}
	]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/simple/requests/" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(page))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	r := resource.NewResourceRepository(resource.WithHTTPClient(srv.Client()))
	_, err := r.DownloadResource(context.Background(), pypiResource(srv.URL+"/simple", "2.32.3"), nil)
	require.ErrorContains(t, err, "404")
	require.ErrorContains(t, err, "distribution")
}

func TestDownloadResource_UnknownCredentialType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("no request expected")
	}))
	t.Cleanup(srv.Close)
	r := resource.NewResourceRepository(resource.WithHTTPClient(srv.Client()))
	bad := &runtime.Raw{Type: runtime.NewVersionedType("Unknown", "v1"), Data: []byte(`{"type":"Unknown/v1"}`)}
	_, err := r.DownloadResource(context.Background(), pypiResource(srv.URL+"/simple", "2.32.3"), bad)
	require.ErrorContains(t, err, "Unknown/v1")
}

func directCreds(props map[string]string) *credv1.DirectCredentials {
	return &credv1.DirectCredentials{
		Type:       runtime.NewVersionedType(credv1.DirectCredentialsType, credv1.Version),
		Properties: props,
	}
}

func TestDownloadResource_Credentials(t *testing.T) {
	t.Run("token username yields Basic auth", func(t *testing.T) {
		var user, pass string
		srv, _ := jsonIndexAuth(t, func(r *http.Request) { user, pass, _ = r.BasicAuth() })
		repo := resource.NewResourceRepository(resource.WithHTTPClient(srv.Client()))
		creds := map[string]string{}
		creds["username"] = "__token__"
		creds["password"] = "test-value"
		_, err := repo.DownloadResource(context.Background(), pypiResource(srv.URL+"/simple", "2.32.3"), directCreds(creds))
		require.NoError(t, err)
		assert.Equal(t, "__token__", user)
		assert.Equal(t, "test-value", pass)
	})
	t.Run("accessToken yields Bearer auth", func(t *testing.T) {
		var auth string
		srv, _ := jsonIndexAuth(t, func(r *http.Request) { auth = r.Header.Get("Authorization") })
		repo := resource.NewResourceRepository(resource.WithHTTPClient(srv.Client()))
		_, err := repo.DownloadResource(context.Background(), pypiResource(srv.URL+"/simple", "2.32.3"),
			directCreds(map[string]string{"accessToken": "legacy"}))
		require.NoError(t, err)
		assert.Equal(t, "Bearer legacy", auth)
	})
	t.Run("password-only credentials error", func(t *testing.T) {
		srv, _ := jsonIndexAuth(t, func(r *http.Request) {})
		repo := resource.NewResourceRepository(resource.WithHTTPClient(srv.Client()))
		creds := map[string]string{}
		creds["password"] = "test-value"
		_, err := repo.DownloadResource(context.Background(), pypiResource(srv.URL+"/simple", "2.32.3"), directCreds(creds))
		require.ErrorContains(t, err, "neither identityToken nor username")
	})
}

// jsonIndexAuth is jsonIndex(serveSig=false) that also runs record on every
// request, so a test can assert the auth headers the repository sent.
func jsonIndexAuth(t *testing.T, record func(*http.Request)) (*httptest.Server, map[string][]byte) {
	t.Helper()
	files := map[string][]byte{"/files/" + wheelName: []byte("WHEELDATA"), "/files/" + sdistName: []byte("SDISTDATA")}
	page := `{"meta":{"api-version":"1.1"},"name":"requests","files":[
	  {"filename":"` + wheelName + `","url":"/files/` + wheelName + `","hashes":{}},
	  {"filename":"` + sdistName + `","url":"/files/` + sdistName + `","hashes":{}}
	]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		record(r)
		if r.URL.Path == "/simple/requests/" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(page))
			return
		}
		if data, ok := files[r.URL.Path]; ok {
			_, _ = w.Write(data)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, files
}

// --- upload ---

type tgzPair struct {
	name string
	data []byte
}

func tgzOf(t *testing.T, entries ...tgzPair) blob.ReadOnlyBlob {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		require.NoError(t, tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: e.name, Mode: 0o644, Size: int64(len(e.data))}))
		_, err := tw.Write(e.data)
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return inmemory.New(bytes.NewReader(buf.Bytes()), inmemory.WithMediaType(resource.MediaTypeTGZ))
}

type recorder struct {
	srv          *httptest.Server
	mu           sync.Mutex
	bodies       map[string][]byte
	contentTypes map[string]string
	rejectStatus int
}

func recordingServer(t *testing.T) *recorder {
	t.Helper()
	rec := &recorder{bodies: map[string][]byte{}, contentTypes: map[string]string{}}
	rec.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		switch req.Method {
		case http.MethodPut:
			if rec.rejectStatus != 0 {
				w.WriteHeader(rec.rejectStatus)
				return
			}
			b, _ := io.ReadAll(req.Body)
			rec.bodies[req.URL.Path] = b
			rec.contentTypes[req.URL.Path] = req.Header.Get("Content-Type")
			w.WriteHeader(http.StatusCreated)
		case http.MethodGet:
			b, ok := rec.bodies[req.URL.Path]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write(b)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(rec.srv.Close)
	return rec
}

func (rec *recorder) repo() *resource.ResourceRepository {
	return resource.NewResourceRepository(resource.WithHTTPClient(rec.srv.Client()))
}

func (rec *recorder) puts() map[string][]byte {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	out := make(map[string][]byte, len(rec.bodies))
	for k, v := range rec.bodies {
		out[k] = v
	}
	return out
}

func (rec *recorder) contentType(path string) string {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return rec.contentTypes[path]
}

func TestUploadResource(t *testing.T) {
	wheel, sig, sdist := []byte("WHEELDATA"), []byte("SIGDATA"), []byte("SDISTDATA")
	dir := "/simple/requests/"

	t.Run("deploys every entry as-is", func(t *testing.T) {
		rec := recordingServer(t)
		res := pypiResource(rec.srv.URL+"/simple", "2.32.3")
		archive := tgzOf(t,
			tgzPair{wheelName, wheel},
			tgzPair{wheelName + ".asc", sig},
			tgzPair{sdistName, sdist},
		)
		updated, err := rec.repo().UploadResource(context.Background(), res, archive, nil)
		require.NoError(t, err)
		assert.Equal(t, res, updated)
		assert.NotSame(t, res, updated)

		puts := rec.puts()
		require.Len(t, puts, 3)
		assert.Equal(t, wheel, puts[dir+wheelName])
		assert.Equal(t, sig, puts[dir+wheelName+".asc"])
		assert.Equal(t, sdist, puts[dir+sdistName])
		assert.Equal(t, "application/octet-stream", rec.contentType(dir+wheelName))
		assert.Equal(t, "application/gzip", rec.contentType(dir+sdistName))
		assert.Equal(t, "text/plain", rec.contentType(dir+wheelName+".asc"))
	})

	t.Run("normalizes project name in the upload path", func(t *testing.T) {
		rec := recordingServer(t)
		res := &descriptor.Resource{Access: &v1alpha1.PyPI{
			Type:     runtime.NewVersionedType(v1alpha1.Type, v1alpha1.Version),
			IndexURL: rec.srv.URL + "/simple",
			Project:  "Foo.Bar",
			Version:  "1.0",
		}}
		_, err := rec.repo().UploadResource(context.Background(), res, tgzOf(t, tgzPair{"Foo_Bar-1.0-py3-none-any.whl", wheel}), nil)
		require.NoError(t, err)
		_, ok := rec.puts()["/simple/foo-bar/Foo_Bar-1.0-py3-none-any.whl"]
		assert.True(t, ok, "upload path uses the normalized project name")
	})

	t.Run("empty archive rejected", func(t *testing.T) {
		rec := recordingServer(t)
		_, err := rec.repo().UploadResource(context.Background(), pypiResource(rec.srv.URL+"/simple", "2.32.3"), tgzOf(t), nil)
		require.ErrorContains(t, err, "no files")
		assert.Empty(t, rec.puts())
	})

	t.Run("path-unsafe entry rejected before any PUT", func(t *testing.T) {
		rec := recordingServer(t)
		_, err := rec.repo().UploadResource(context.Background(), pypiResource(rec.srv.URL+"/simple", "2.32.3"),
			tgzOf(t, tgzPair{wheelName, wheel}, tgzPair{"../evil.whl", wheel}), nil)
		require.ErrorContains(t, err, "plain file name")
		assert.Empty(t, rec.puts())
	})

	t.Run("duplicate entry rejected", func(t *testing.T) {
		rec := recordingServer(t)
		_, err := rec.repo().UploadResource(context.Background(), pypiResource(rec.srv.URL+"/simple", "2.32.3"),
			tgzOf(t, tgzPair{wheelName, wheel}, tgzPair{wheelName, wheel}), nil)
		require.ErrorContains(t, err, "more than once")
		assert.Empty(t, rec.puts())
	})

	t.Run("signature without its file rejected", func(t *testing.T) {
		rec := recordingServer(t)
		_, err := rec.repo().UploadResource(context.Background(), pypiResource(rec.srv.URL+"/simple", "2.32.3"),
			tgzOf(t, tgzPair{wheelName + ".asc", sig}), nil)
		require.ErrorContains(t, err, "without its file")
		assert.Empty(t, rec.puts())
	})

	t.Run("non-tgz content rejected", func(t *testing.T) {
		rec := recordingServer(t)
		_, err := rec.repo().UploadResource(context.Background(), pypiResource(rec.srv.URL+"/simple", "2.32.3"),
			inmemory.New(bytes.NewReader(wheel)), nil)
		require.ErrorContains(t, err, "not an application/x-tgz archive")
		assert.Empty(t, rec.puts())
	})

	t.Run("a refused PUT stops the upload", func(t *testing.T) {
		rec := recordingServer(t)
		rec.rejectStatus = http.StatusUnauthorized
		_, err := rec.repo().UploadResource(context.Background(), pypiResource(rec.srv.URL+"/simple", "2.32.3"),
			tgzOf(t, tgzPair{wheelName, wheel}), nil)
		require.ErrorContains(t, err, "unexpected status 401")
	})
}

func TestDownloadThenUpload_RoundTrip(t *testing.T) {
	src, _ := jsonIndex(t, true)
	target := recordingServer(t)

	downloaded, err := resource.NewResourceRepository(resource.WithHTTPClient(src.Client())).
		DownloadResource(context.Background(), pypiResource(src.URL+"/simple", "2.32.3"), nil)
	require.NoError(t, err)

	_, err = target.repo().UploadResource(context.Background(), pypiResource(target.srv.URL+"/simple", "2.32.3"), downloaded, nil)
	require.NoError(t, err)

	puts := target.puts()
	assert.Equal(t, []byte("WHEELDATA"), puts["/simple/requests/"+wheelName])
	assert.Equal(t, []byte("SIGDATA"), puts["/simple/requests/"+wheelName+".asc"])
	assert.Equal(t, []byte("SDISTDATA"), puts["/simple/requests/"+sdistName])
	assert.Len(t, puts, 3)
}

// --- tgz helpers ---

func readTgzNames(t *testing.T, b blob.ReadOnlyBlob) []string {
	t.Helper()
	rc, err := b.ReadCloser()
	require.NoError(t, err)
	defer rc.Close()
	gz, err := gzip.NewReader(rc)
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

func readTgzEntries(t *testing.T, b blob.ReadOnlyBlob) map[string][]byte {
	t.Helper()
	rc, err := b.ReadCloser()
	require.NoError(t, err)
	defer rc.Close()
	gz, err := gzip.NewReader(rc)
	require.NoError(t, err)
	tr := tar.NewReader(gz)
	entries := map[string][]byte{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		data, err := io.ReadAll(tr)
		require.NoError(t, err)
		entries[h.Name] = data
	}
	return entries
}
