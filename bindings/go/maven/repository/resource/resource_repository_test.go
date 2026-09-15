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
	"ocm.software/open-component-model/bindings/go/maven/repository/resource"
	mavenaccess "ocm.software/open-component-model/bindings/go/maven/spec/access"
	"ocm.software/open-component-model/bindings/go/maven/spec/access/v2alpha1"
	credsv1 "ocm.software/open-component-model/bindings/go/maven/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var jarOnly = []v2alpha1.Artifact{{Extension: "jar"}}

// mavenResource builds a resource for com.example:lib:<version> at repoURL.
func mavenResource(repoURL, version string, artifacts ...v2alpha1.Artifact) *descriptor.Resource {
	if len(artifacts) == 0 {
		artifacts = jarOnly
	}
	return &descriptor.Resource{Access: &v2alpha1.Maven{
		Type:       runtime.NewVersionedType(v2alpha1.Type, v2alpha1.Version),
		RepoURL:    repoURL,
		GroupID:    "com.example",
		ArtifactID: "lib",
		Version:    version,
		Artifacts:  artifacts,
	}}
}

func mediaTypeOf(t *testing.T, b blob.ReadOnlyBlob) string {
	t.Helper()
	mt, ok := b.(blob.MediaTypeAware).MediaType()
	require.True(t, ok)
	return mt
}

func TestGetResourceRepositoryScheme(t *testing.T) {
	r := resource.NewResourceRepository()
	assert.Same(t, mavenaccess.Scheme, r.GetResourceRepositoryScheme())
}

func TestGetResourceCredentialConsumerIdentity(t *testing.T) {
	r := resource.NewResourceRepository()
	id, err := r.GetResourceCredentialConsumerIdentity(context.Background(), mavenResource("https://maven.example.com/repo", "1"))
	require.NoError(t, err)
	assert.Equal(t, "MavenRepository", id[runtime.IdentityAttributeType])
	assert.Equal(t, "maven.example.com", id[runtime.IdentityAttributeHostname])
}

func TestConvertAccess(t *testing.T) {
	r := resource.NewResourceRepository()
	t.Run("nil resource", func(t *testing.T) {
		_, err := r.GetResourceCredentialConsumerIdentity(context.Background(), nil)
		require.ErrorContains(t, err, "access is required")
	})
	t.Run("nil access", func(t *testing.T) {
		_, err := r.GetResourceCredentialConsumerIdentity(context.Background(), &descriptor.Resource{})
		require.ErrorContains(t, err, "access is required")
	})
	t.Run("invalid spec is rejected before any request", func(t *testing.T) {
		res := mavenResource("https://maven.example.com/repo", "1")
		res.Access.(*v2alpha1.Maven).Artifacts = nil
		_, err := r.DownloadResource(context.Background(), res, nil)
		require.ErrorContains(t, err, "invalid maven access")
		require.ErrorContains(t, err, "artifacts")
	})
	t.Run("old maven/v1 type is not accepted", func(t *testing.T) {
		res := &descriptor.Resource{Access: &runtime.Raw{
			Type: runtime.NewVersionedType("maven", "v1"),
			Data: []byte(`{"type":"maven/v1","repoUrl":"https://r","groupId":"g","artifactId":"a","version":"1"}`),
		}}
		_, err := r.DownloadResource(context.Background(), res, nil)
		require.ErrorContains(t, err, "error converting access")
	})
}

func TestDownloadResource(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/com/example/lib/1.2.3/lib-1.2.3.jar" {
			_, _ = w.Write([]byte("JARDATA"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	r := resource.NewResourceRepository(resource.WithHTTPClient(srv.Client()))

	t.Run("single file without siblings is still a tgz with one entry", func(t *testing.T) {
		b, err := r.DownloadResource(context.Background(), mavenResource(srv.URL, "1.2.3"), nil)
		require.NoError(t, err)
		assert.Equal(t, resource.MediaTypeTGZ, mediaTypeOf(t, b))
		assert.Equal(t, []string{"lib-1.2.3.jar"}, readTgzNames(t, b))
		assert.Equal(t, []byte("JARDATA"), readTgzEntries(t, b)["lib-1.2.3.jar"])
	})

	t.Run("404 errors", func(t *testing.T) {
		_, err := r.DownloadResource(context.Background(), mavenResource(srv.URL, "9.9.9"), nil)
		require.ErrorContains(t, err, "404")
	})

	t.Run("a missing file fails the whole multi-file download", func(t *testing.T) {
		res := mavenResource(srv.URL, "1.2.3", v2alpha1.Artifact{Extension: "jar"}, v2alpha1.Artifact{Extension: "pom"})
		_, err := r.DownloadResource(context.Background(), res, nil)
		require.ErrorContains(t, err, "lib-1.2.3.pom")
	})
}

func TestDownloadResource_SeveralFiles_Tgz(t *testing.T) {
	files := map[string][]byte{
		"/com/example/lib/1.2.3/lib-1.2.3.pom":         []byte("POMDATA"),
		"/com/example/lib/1.2.3/lib-1.2.3.jar":         []byte("JARDATA"),
		"/com/example/lib/1.2.3/lib-1.2.3-sources.jar": []byte("SRCDATA"),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if data, ok := files[req.URL.Path]; ok {
			_, _ = w.Write(data)
			return
		}
		http.NotFound(w, req) // no siblings
	}))
	t.Cleanup(srv.Close)

	r := resource.NewResourceRepository(resource.WithHTTPClient(srv.Client()))
	res := mavenResource(srv.URL, "1.2.3",
		v2alpha1.Artifact{Extension: "pom"},
		v2alpha1.Artifact{Extension: "jar"},
		v2alpha1.Artifact{Extension: "jar", Classifier: "sources"},
	)
	b, err := r.DownloadResource(context.Background(), res, nil)
	require.NoError(t, err)
	assert.Equal(t, resource.MediaTypeTGZ, mediaTypeOf(t, b))

	entries := readTgzEntries(t, b)
	require.Len(t, entries, 3)
	assert.Equal(t, []byte("POMDATA"), entries["lib-1.2.3.pom"])
	assert.Equal(t, []byte("JARDATA"), entries["lib-1.2.3.jar"])
	assert.Equal(t, []byte("SRCDATA"), entries["lib-1.2.3-sources.jar"])
}

func TestDownloadResource_Snapshot_Tgz(t *testing.T) {
	jar := []byte("JARDATA")
	src := []byte("SRCDATA")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/maven2/com/example/lib/1.0-SNAPSHOT/maven-metadata.xml":
			_, _ = w.Write([]byte(`<metadata><versioning><snapshotVersions>` +
				`<snapshotVersion><extension>jar</extension><value>1.0-20240101.120000-3</value></snapshotVersion>` +
				`<snapshotVersion><classifier>sources</classifier><extension>jar</extension><value>1.0-20240101.120000-3</value></snapshotVersion>` +
				`</snapshotVersions></versioning></metadata>`))
		case "/maven2/com/example/lib/1.0-SNAPSHOT/lib-1.0-20240101.120000-3.jar":
			_, _ = w.Write(jar)
		case "/maven2/com/example/lib/1.0-SNAPSHOT/lib-1.0-20240101.120000-3-sources.jar":
			_, _ = w.Write(src)
		default:
			http.NotFound(w, req) // no siblings
		}
	}))
	t.Cleanup(srv.Close)

	r := resource.NewResourceRepository(resource.WithHTTPClient(srv.Client()))
	res := mavenResource(srv.URL+"/maven2", "1.0-SNAPSHOT",
		v2alpha1.Artifact{Extension: "jar"},
		v2alpha1.Artifact{Extension: "jar", Classifier: "sources"},
	)
	b, err := r.DownloadResource(context.Background(), res, nil)
	require.NoError(t, err)
	assert.Equal(t, resource.MediaTypeTGZ, mediaTypeOf(t, b))

	entries := readTgzEntries(t, b)
	require.Len(t, entries, 2)
	assert.Equal(t, jar, entries["lib-1.0-20240101.120000-3.jar"])
	assert.Equal(t, src, entries["lib-1.0-20240101.120000-3-sources.jar"])
}

func TestDownloadResource_Siblings(t *testing.T) {
	const (
		jar = "/com/example/lib/1.2.3/lib-1.2.3.jar"
		pom = "/com/example/lib/1.2.3/lib-1.2.3.pom"
	)
	jarData, sigData, sha1Data := []byte("JARDATA"), []byte("-----BEGIN PGP SIGNATURE-----"), []byte("not-a-real-checksum")

	// serve the jar with a signature and a (wrong) sha1, and an unsigned pom;
	// the signature's own status is controlled per subtest.
	newSrv := func(sigStatus int) *httptest.Server {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			switch req.URL.Path {
			case jar:
				_, _ = w.Write(jarData)
			case jar + ".sha1":
				_, _ = w.Write(sha1Data)
			case jar + ".asc":
				if sigStatus != http.StatusOK {
					w.WriteHeader(sigStatus)
					return
				}
				_, _ = w.Write(sigData)
			case pom:
				_, _ = w.Write([]byte("POMDATA"))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		t.Cleanup(srv.Close)
		return srv
	}

	t.Run("siblings follow their file, in suffix order, stored as served", func(t *testing.T) {
		srv := newSrv(http.StatusOK)
		r := resource.NewResourceRepository(resource.WithHTTPClient(srv.Client()))
		res := mavenResource(srv.URL, "1.2.3", v2alpha1.Artifact{Extension: "jar"}, v2alpha1.Artifact{Extension: "pom"})
		b, err := r.DownloadResource(context.Background(), res, nil)
		require.NoError(t, err)
		assert.Equal(t, resource.MediaTypeTGZ, mediaTypeOf(t, b))
		assert.Equal(t, []string{"lib-1.2.3.jar", "lib-1.2.3.jar.asc", "lib-1.2.3.jar.sha1", "lib-1.2.3.pom"}, readTgzNames(t, b))
		entries := readTgzEntries(t, b)
		assert.Equal(t, sigData, entries["lib-1.2.3.jar.asc"])
		assert.Equal(t, sha1Data, entries["lib-1.2.3.jar.sha1"], "a checksum that does not match the file is still stored as-is: nothing is verified")
	})

	t.Run("refused sibling is an error, not an absent one", func(t *testing.T) {
		srv := newSrv(http.StatusForbidden)
		r := resource.NewResourceRepository(resource.WithHTTPClient(srv.Client()))
		_, err := r.DownloadResource(context.Background(), mavenResource(srv.URL, "1.2.3"), nil)
		require.ErrorContains(t, err, "sibling")
		require.ErrorContains(t, err, "403")
	})
}

// tgzOf packs name/data pairs into the archive shape DownloadResource produces.
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

type tgzPair struct {
	name string
	data []byte
}

// recorder is a Maven repository stub that stores PUT bodies by path and
// serves them back on GET.
type recorder struct {
	srv          *httptest.Server
	mu           sync.Mutex
	bodies       map[string][]byte
	contentTypes map[string]string
	// rejectStatus, when set, refuses every PUT with that status.
	rejectStatus int
	// allowOnce refuses a PUT to a path that already holds content with 409,
	// like a Nexus hosted repository with the ALLOW_ONCE write policy.
	allowOnce bool
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
			if _, exists := rec.bodies[req.URL.Path]; exists && rec.allowOnce {
				w.WriteHeader(http.StatusConflict)
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

// repo returns a resource repository talking to the stub.
func (rec *recorder) repo() *resource.ResourceRepository {
	return resource.NewResourceRepository(resource.WithHTTPClient(rec.srv.Client()))
}

// puts returns a copy of the stored PUT bodies by path.
func (rec *recorder) puts() map[string][]byte {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	out := make(map[string][]byte, len(rec.bodies))
	for k, v := range rec.bodies {
		out[k] = v
	}
	return out
}

// contentType returns the Content-Type the stub received for path.
func (rec *recorder) contentType(path string) string {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return rec.contentTypes[path]
}

func TestUploadResource(t *testing.T) {
	jar, pom, sig := []byte("JARDATA"), []byte("POMDATA"), []byte("SIGDATA")
	dir := "/com/example/lib/1.2.3/"

	t.Run("deploys every archive entry as-is, siblings included", func(t *testing.T) {
		rec := recordingServer(t)
		res := mavenResource(rec.srv.URL, "1.2.3", v2alpha1.Artifact{Extension: "jar"}, v2alpha1.Artifact{Extension: "pom"})
		archive := tgzOf(t,
			tgzPair{"lib-1.2.3.jar", jar},
			tgzPair{"lib-1.2.3.jar.asc", sig},
			tgzPair{"lib-1.2.3.jar.sha1", []byte("as-served")},
			tgzPair{"lib-1.2.3.pom", pom},
		)

		updated, err := rec.repo().UploadResource(context.Background(), res, archive, nil)
		require.NoError(t, err)
		assert.Equal(t, res, updated)
		assert.NotSame(t, res, updated)

		puts := rec.puts()
		require.Len(t, puts, 4, "one PUT per archive entry, no generated checksums")
		assert.Equal(t, jar, puts[dir+"lib-1.2.3.jar"])
		assert.Equal(t, sig, puts[dir+"lib-1.2.3.jar.asc"])
		assert.Equal(t, []byte("as-served"), puts[dir+"lib-1.2.3.jar.sha1"], "a checksum entry is deployed verbatim, not recomputed")
		assert.Equal(t, pom, puts[dir+"lib-1.2.3.pom"])

		assert.Equal(t, "application/java-archive", rec.contentType(dir+"lib-1.2.3.jar"))
		assert.Equal(t, "application/xml", rec.contentType(dir+"lib-1.2.3.pom"))
		assert.Equal(t, "text/plain", rec.contentType(dir+"lib-1.2.3.jar.asc"))
		assert.Equal(t, "text/plain", rec.contentType(dir+"lib-1.2.3.jar.sha1"))
	})

	t.Run("a single-file archive works", func(t *testing.T) {
		rec := recordingServer(t)
		res := mavenResource(rec.srv.URL, "1.2.3", v2alpha1.Artifact{Extension: "zip", Classifier: "dist"})
		_, err := rec.repo().UploadResource(context.Background(), res, tgzOf(t, tgzPair{"lib-1.2.3-dist.zip", jar}), nil)
		require.NoError(t, err)
		assert.Equal(t, jar, rec.puts()[dir+"lib-1.2.3-dist.zip"])
	})

	t.Run("a listed file whose extension is a sibling suffix is a file, not a sibling", func(t *testing.T) {
		rec := recordingServer(t)
		res := mavenResource(rec.srv.URL, "1.2.3", v2alpha1.Artifact{Extension: "asc"})
		archive := tgzOf(t, tgzPair{"lib-1.2.3.asc", sig}, tgzPair{"lib-1.2.3.asc.sha1", []byte("sum")})
		_, err := rec.repo().UploadResource(context.Background(), res, archive, nil)
		require.NoError(t, err)
		assert.Len(t, rec.puts(), 2)
	})

	t.Run("archive missing a listed file is rejected before any PUT", func(t *testing.T) {
		rec := recordingServer(t)
		res := mavenResource(rec.srv.URL, "1.2.3", v2alpha1.Artifact{Extension: "jar"}, v2alpha1.Artifact{Extension: "pom"})
		_, err := rec.repo().UploadResource(context.Background(), res, tgzOf(t, tgzPair{"lib-1.2.3.jar", jar}), nil)
		require.ErrorContains(t, err, `missing "lib-1.2.3.pom"`)
		assert.Empty(t, rec.puts())
	})

	t.Run("archive with an unlisted entry is rejected before any PUT", func(t *testing.T) {
		rec := recordingServer(t)
		archive := tgzOf(t, tgzPair{"lib-1.2.3.jar", jar}, tgzPair{"../evil.jar", jar})
		_, err := rec.repo().UploadResource(context.Background(), mavenResource(rec.srv.URL, "1.2.3"), archive, nil)
		require.ErrorContains(t, err, `"../evil.jar" is not listed`)
		assert.Empty(t, rec.puts())
	})

	t.Run("a signature without its file is rejected", func(t *testing.T) {
		rec := recordingServer(t)
		_, err := rec.repo().UploadResource(context.Background(), mavenResource(rec.srv.URL, "1.2.3"), tgzOf(t, tgzPair{"lib-1.2.3.jar.asc", sig}), nil)
		require.ErrorContains(t, err, `missing "lib-1.2.3.jar"`)
	})

	t.Run("content that is not a tgz is rejected", func(t *testing.T) {
		rec := recordingServer(t)
		_, err := rec.repo().UploadResource(context.Background(), mavenResource(rec.srv.URL, "1.2.3"), inmemory.New(bytes.NewReader(jar)), nil)
		require.ErrorContains(t, err, "not an application/x-tgz archive")
		assert.Empty(t, rec.puts())
	})

	t.Run("a directory entry is rejected before any PUT", func(t *testing.T) {
		rec := recordingServer(t)
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gz)
		require.NoError(t, tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "lib-1.2.3.jar", Mode: 0o755}))
		require.NoError(t, tw.Close())
		require.NoError(t, gz.Close())
		archive := inmemory.New(bytes.NewReader(buf.Bytes()), inmemory.WithMediaType(resource.MediaTypeTGZ))

		_, err := rec.repo().UploadResource(context.Background(), mavenResource(rec.srv.URL, "1.2.3"), archive, nil)
		require.ErrorContains(t, err, "not a regular file")
		assert.Empty(t, rec.puts())
	})

	t.Run("a truncated archive is rejected before any PUT", func(t *testing.T) {
		rec := recordingServer(t)
		whole := readAll(t, tgzOf(t, tgzPair{"lib-1.2.3.jar", jar}))
		truncated := inmemory.New(bytes.NewReader(whole[:len(whole)/2]), inmemory.WithMediaType(resource.MediaTypeTGZ))

		_, err := rec.repo().UploadResource(context.Background(), mavenResource(rec.srv.URL, "1.2.3"), truncated, nil)
		require.Error(t, err)
		assert.Empty(t, rec.puts())
	})

	t.Run("SNAPSHOT upload is rejected", func(t *testing.T) {
		rec := recordingServer(t)
		_, err := rec.repo().UploadResource(context.Background(), mavenResource(rec.srv.URL, "1.0-SNAPSHOT"), tgzOf(t, tgzPair{"lib-1.0-SNAPSHOT.jar", jar}), nil)
		require.ErrorContains(t, err, "SNAPSHOT")
		assert.Empty(t, rec.puts())
	})

	t.Run("LATEST and RELEASE uploads are rejected", func(t *testing.T) {
		for _, version := range []string{"LATEST", "RELEASE"} {
			rec := recordingServer(t)
			_, err := rec.repo().UploadResource(context.Background(), mavenResource(rec.srv.URL, version), tgzOf(t, tgzPair{"lib-1.2.3.jar", jar}), nil)
			require.ErrorContains(t, err, version)
			require.ErrorContains(t, err, "concrete version")
			assert.Empty(t, rec.puts())
		}
	})

	t.Run("a refused PUT fails the upload and stops it", func(t *testing.T) {
		rec := recordingServer(t)
		rec.rejectStatus = http.StatusUnauthorized
		res := mavenResource(rec.srv.URL, "1.2.3", v2alpha1.Artifact{Extension: "jar"}, v2alpha1.Artifact{Extension: "pom"})
		archive := tgzOf(t, tgzPair{"lib-1.2.3.jar", jar}, tgzPair{"lib-1.2.3.pom", pom})
		_, err := rec.repo().UploadResource(context.Background(), res, archive, nil)
		require.ErrorContains(t, err, "unexpected status 401")
		require.ErrorContains(t, err, "lib-1.2.3.jar")
		assert.Empty(t, rec.puts(), "the entry after the failed one must not be written")
	})

	t.Run("redeploying a release the repository already holds is an error", func(t *testing.T) {
		rec := recordingServer(t)
		rec.allowOnce = true
		res := mavenResource(rec.srv.URL, "1.2.3")
		_, err := rec.repo().UploadResource(context.Background(), res, tgzOf(t, tgzPair{"lib-1.2.3.jar", jar}), nil)
		require.NoError(t, err)
		_, err = rec.repo().UploadResource(context.Background(), res, tgzOf(t, tgzPair{"lib-1.2.3.jar", jar}), nil)
		require.ErrorContains(t, err, "unexpected status 409")
	})
}

// TestDownloadThenUpload_RoundTrip is the transfer scenario: what one
// repository serves is deployed unchanged into another.
func TestDownloadThenUpload_RoundTrip(t *testing.T) {
	jar, sig, pom, sum := []byte("JARDATA"), []byte("SIGDATA"), []byte("POMDATA"), []byte("SHA1-AS-SERVED")
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/com/example/lib/1.2.3/lib-1.2.3.jar":
			_, _ = w.Write(jar)
		case "/com/example/lib/1.2.3/lib-1.2.3.jar.asc":
			_, _ = w.Write(sig)
		case "/com/example/lib/1.2.3/lib-1.2.3.jar.sha1":
			_, _ = w.Write(sum)
		case "/com/example/lib/1.2.3/lib-1.2.3.pom":
			_, _ = w.Write(pom)
		default:
			http.NotFound(w, req)
		}
	}))
	t.Cleanup(source.Close)
	target := recordingServer(t)

	artifacts := []v2alpha1.Artifact{{Extension: "jar"}, {Extension: "pom"}}
	downloaded, err := resource.NewResourceRepository(resource.WithHTTPClient(source.Client())).
		DownloadResource(context.Background(), mavenResource(source.URL, "1.2.3", artifacts...), nil)
	require.NoError(t, err)

	_, err = target.repo().UploadResource(context.Background(), mavenResource(target.srv.URL, "1.2.3", artifacts...), downloaded, nil)
	require.NoError(t, err)

	puts := target.puts()
	assert.Equal(t, jar, puts["/com/example/lib/1.2.3/lib-1.2.3.jar"])
	assert.Equal(t, sig, puts["/com/example/lib/1.2.3/lib-1.2.3.jar.asc"])
	assert.Equal(t, pom, puts["/com/example/lib/1.2.3/lib-1.2.3.pom"])
	assert.Equal(t, sum, puts["/com/example/lib/1.2.3/lib-1.2.3.jar.sha1"], "the source's checksum file is mirrored unchanged")
	assert.Len(t, puts, 4)
}

// directCreds builds a *credv1.DirectCredentials with the given properties.
func directCreds(props map[string]string) *credv1.DirectCredentials {
	return &credv1.DirectCredentials{
		Type:       runtime.NewVersionedType(credv1.DirectCredentialsType, credv1.Version),
		Properties: props,
	}
}

func TestDownloadResourceWithCredentials(t *testing.T) {
	t.Run("direct credentials with old OCM accessToken yield Bearer auth", func(t *testing.T) {
		var receivedAuth string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			receivedAuth = req.Header.Get("Authorization")
			if req.URL.Path == "/com/example/lib/1.2.3/lib-1.2.3.jar" {
				_, _ = w.Write([]byte("JARDATA"))
				return
			}
			w.WriteHeader(http.StatusNotFound) // no siblings
		}))
		t.Cleanup(srv.Close)

		r := resource.NewResourceRepository(resource.WithHTTPClient(srv.Client()))
		_, err := r.DownloadResource(context.Background(), mavenResource(srv.URL, "1.2.3"), directCreds(map[string]string{"accessToken": "mytoken123"}))
		require.NoError(t, err)
		assert.Equal(t, "Bearer mytoken123", receivedAuth)
	})

	t.Run("username+password yields Basic auth", func(t *testing.T) {
		var receivedUser, receivedPass string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			receivedUser, receivedPass, _ = req.BasicAuth()
			if req.URL.Path == "/com/example/lib/1.2.3/lib-1.2.3.jar" {
				_, _ = w.Write([]byte("JARDATA"))
				return
			}
			w.WriteHeader(http.StatusNotFound) // no siblings
		}))
		t.Cleanup(srv.Close)

		r := resource.NewResourceRepository(resource.WithHTTPClient(srv.Client()))
		_, err := r.DownloadResource(context.Background(), mavenResource(srv.URL, "1.2.3"), directCreds(map[string]string{"username": "alice", "password": "s3cr3t"}))
		require.NoError(t, err)
		assert.Equal(t, "alice", receivedUser)
		assert.Equal(t, "s3cr3t", receivedPass)
	})

	t.Run("typed MavenCredentials with identityToken yield Bearer auth", func(t *testing.T) {
		var receivedAuth string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			receivedAuth = req.Header.Get("Authorization")
			if req.URL.Path == "/com/example/lib/1.2.3/lib-1.2.3.jar" {
				_, _ = w.Write([]byte("JARDATA"))
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		t.Cleanup(srv.Close)

		r := resource.NewResourceRepository(resource.WithHTTPClient(srv.Client()))
		creds := &credsv1.MavenCredentials{Type: runtime.NewVersionedType(credsv1.MavenCredentialsType, credsv1.Version), IdentityToken: "typed-token"}
		_, err := r.DownloadResource(context.Background(), mavenResource(srv.URL, "1.2.3"), creds)
		require.NoError(t, err)
		assert.Equal(t, "Bearer typed-token", receivedAuth)
	})

	t.Run("a property bag without maven keys is an anonymous request", func(t *testing.T) {
		var receivedAuth string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			receivedAuth = req.Header.Get("Authorization")
			if req.URL.Path == "/com/example/lib/1.2.3/lib-1.2.3.jar" {
				_, _ = w.Write([]byte("JARDATA"))
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		t.Cleanup(srv.Close)

		r := resource.NewResourceRepository(resource.WithHTTPClient(srv.Client()))
		_, err := r.DownloadResource(context.Background(), mavenResource(srv.URL, "1.2.3"), directCreds(map[string]string{"certificate": "pem"}))
		require.NoError(t, err)
		assert.Empty(t, receivedAuth)
	})

	t.Run("password-only credentials return error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			_, _ = w.Write([]byte("JARDATA"))
		}))
		t.Cleanup(srv.Close)

		r := resource.NewResourceRepository(resource.WithHTTPClient(srv.Client()))
		_, err := r.DownloadResource(context.Background(), mavenResource(srv.URL, "1.2.3"), directCreds(map[string]string{"password": "s3cr3t"}))
		require.ErrorContains(t, err, "neither identityToken nor username")
	})

	t.Run("unknown credential type is rejected before any request", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			t.Error("no request expected")
		}))
		t.Cleanup(srv.Close)

		r := resource.NewResourceRepository(resource.WithHTTPClient(srv.Client()))
		bad := &runtime.Raw{Type: runtime.NewVersionedType("Unknown", "v1"), Data: []byte(`{"type":"Unknown/v1"}`)}
		_, err := r.DownloadResource(context.Background(), mavenResource(srv.URL, "1.2.3"), bad)
		require.ErrorContains(t, err, "Unknown/v1")
	})
}

// readAll returns the whole content of b.
func readAll(t *testing.T, b blob.ReadOnlyBlob) []byte {
	t.Helper()
	rc, err := b.ReadCloser()
	require.NoError(t, err)
	defer rc.Close()
	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	return data
}

// readTgzNames returns the entry names of a gzip-compressed tar blob in
// archive order.
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

// readTgzEntries reads a gzip-compressed tar blob and returns a map of entry
// name to its raw content.
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
