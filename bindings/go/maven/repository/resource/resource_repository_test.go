package resource_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"fmt"
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
	v1 "ocm.software/open-component-model/bindings/go/maven/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func mavenResource(m *v1.Maven) *descriptor.Resource {
	m.Type = runtime.NewVersionedType(v1.Type, v1.Version)
	return &descriptor.Resource{Access: m}
}

func TestGetResourceRepositoryScheme(t *testing.T) {
	r := resource.NewResourceRepository()
	assert.Same(t, mavenaccess.Scheme, r.GetResourceRepositoryScheme())
}

func TestGetResourceCredentialConsumerIdentity(t *testing.T) {
	r := resource.NewResourceRepository()
	res := mavenResource(&v1.Maven{RepoURL: "https://maven.example.com/repo", GroupID: "g", ArtifactID: "a", Version: "1"})
	id, err := r.GetResourceCredentialConsumerIdentity(context.Background(), res)
	require.NoError(t, err)
	assert.Equal(t, "MavenRepository", id[runtime.IdentityAttributeType])
	assert.Equal(t, "maven.example.com", id[runtime.IdentityAttributeHostname])
}

func TestConvertAccessNilGuards(t *testing.T) {
	r := resource.NewResourceRepository()
	_, err := r.GetResourceCredentialConsumerIdentity(context.Background(), nil)
	require.Error(t, err)
	_, err = r.GetResourceCredentialConsumerIdentity(context.Background(), &descriptor.Resource{})
	require.Error(t, err)
}

func TestDownloadResource(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/com/example/lib/1.2.3/lib-1.2.3.jar" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("JARDATA"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	r := resource.NewResourceRepository(resource.WithHTTPClient(srv.Client()))

	t.Run("downloads jar with default extension", func(t *testing.T) {
		res := mavenResource(&v1.Maven{RepoURL: srv.URL, GroupID: "com.example", ArtifactID: "lib", Version: "1.2.3"})
		b, err := r.DownloadResource(context.Background(), res, nil)
		require.NoError(t, err)
		rc, err := b.ReadCloser()
		require.NoError(t, err)
		defer rc.Close()
		data, err := io.ReadAll(rc)
		require.NoError(t, err)
		assert.Equal(t, "JARDATA", string(data))
		mt, ok := b.(interface{ MediaType() (string, bool) }).MediaType()
		require.True(t, ok)
		assert.Equal(t, "application/java-archive", mt)
	})

	t.Run("404 errors", func(t *testing.T) {
		res := mavenResource(&v1.Maven{RepoURL: srv.URL, GroupID: "com.example", ArtifactID: "missing", Version: "9.9.9"})
		_, err := r.DownloadResource(context.Background(), res, nil)
		require.ErrorContains(t, err, "404")
	})
}

func TestUploadResource(t *testing.T) {
	puts := map[string]string{}
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		require.Equal(t, http.MethodPut, req.Method)
		body, _ := io.ReadAll(req.Body)
		mu.Lock()
		puts[req.URL.Path] = string(body)
		mu.Unlock()
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(srv.Close)

	r := resource.NewResourceRepository(resource.WithHTTPClient(srv.Client()))
	res := mavenResource(&v1.Maven{RepoURL: srv.URL, GroupID: "com.example", ArtifactID: "lib", Version: "1.2.3"})
	content := inmemory.New(bytes.NewReader([]byte("JARDATA")))

	updated, err := r.UploadResource(context.Background(), res, content, nil)
	require.NoError(t, err)
	require.NotNil(t, updated)

	base := "/com/example/lib/1.2.3/lib-1.2.3.jar"
	assert.Equal(t, "JARDATA", puts[base])
	assert.Equal(t, fmt.Sprintf("%x", sha1.Sum([]byte("JARDATA"))), puts[base+".sha1"])
	assert.Equal(t, fmt.Sprintf("%x", md5.Sum([]byte("JARDATA"))), puts[base+".md5"])
	assert.Equal(t, fmt.Sprintf("%x", sha256.Sum256([]byte("JARDATA"))), puts[base+".sha256"])

	// returned resource carries the maven access + media type
	var got v1.Maven
	require.NoError(t, mavenaccess.Scheme.Convert(updated.Access, &got))
	require.NotNil(t, got.MediaType)
	assert.Equal(t, "application/java-archive", *got.MediaType)
}

// directCreds builds a *credv1.DirectCredentials with the given properties.
func directCreds(props map[string]string) *credv1.DirectCredentials {
	return &credv1.DirectCredentials{
		Type:       runtime.NewVersionedType(credv1.DirectCredentialsType, credv1.Version),
		Properties: props,
	}
}

func TestDownloadResourceWithCredentials(t *testing.T) {
	jarPath := "/com/example/lib/1.2.3/lib-1.2.3.jar"

	t.Run("accessToken yields Bearer auth", func(t *testing.T) {
		var receivedAuth string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			receivedAuth = req.Header.Get("Authorization")
			if req.URL.Path == "/com/example/lib/1.2.3/lib-1.2.3.jar" {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("JARDATA"))
				return
			}
			w.WriteHeader(http.StatusNotFound) // .sha1 -> soft-pass
		}))
		t.Cleanup(srv.Close)

		r := resource.NewResourceRepository(resource.WithHTTPClient(srv.Client()))
		res := mavenResource(&v1.Maven{RepoURL: srv.URL, GroupID: "com.example", ArtifactID: "lib", Version: "1.2.3"})
		creds := directCreds(map[string]string{"accessToken": "mytoken123"})

		_, err := r.DownloadResource(context.Background(), res, creds)
		require.NoError(t, err)
		assert.Equal(t, "Bearer mytoken123", receivedAuth)
	})

	t.Run("username+password yields Basic auth", func(t *testing.T) {
		var receivedUser, receivedPass string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			receivedUser, receivedPass, _ = req.BasicAuth()
			if req.URL.Path == "/com/example/lib/1.2.3/lib-1.2.3.jar" {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("JARDATA"))
				return
			}
			w.WriteHeader(http.StatusNotFound) // .sha1 -> soft-pass
		}))
		t.Cleanup(srv.Close)

		r := resource.NewResourceRepository(resource.WithHTTPClient(srv.Client()))
		res := mavenResource(&v1.Maven{RepoURL: srv.URL, GroupID: "com.example", ArtifactID: "lib", Version: "1.2.3"})
		creds := directCreds(map[string]string{"username": "alice", "password": "s3cr3t"})

		_, err := r.DownloadResource(context.Background(), res, creds)
		require.NoError(t, err)
		assert.Equal(t, "alice", receivedUser)
		assert.Equal(t, "s3cr3t", receivedPass)
	})

	t.Run("password-only credentials return error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("JARDATA"))
		}))
		t.Cleanup(srv.Close)

		r := resource.NewResourceRepository(resource.WithHTTPClient(srv.Client()))
		res := mavenResource(&v1.Maven{RepoURL: srv.URL + jarPath, GroupID: "com.example", ArtifactID: "lib", Version: "1.2.3"})
		creds := directCreds(map[string]string{"password": "s3cr3t"})

		_, err := r.DownloadResource(context.Background(), res, creds)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "neither accessToken nor username")
	})
}

func TestDownloadResourceVerification(t *testing.T) {
	jar := "/com/example/lib/1.2.3/lib-1.2.3.jar"
	body := []byte("JARDATA")
	// serve the jar; serve its .sha1 with a value controlled per-subtest.
	newSrv := func(sha1Body string, sha1Status int) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			switch req.URL.Path {
			case jar:
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(body)
			case jar + ".sha1":
				if sha1Status != http.StatusOK {
					w.WriteHeader(sha1Status)
					return
				}
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(sha1Body))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
	}

	t.Run("valid checksum passes", func(t *testing.T) {
		srv := newSrv(fmt.Sprintf("%x", sha1.Sum(body)), http.StatusOK)
		t.Cleanup(srv.Close)
		r := resource.NewResourceRepository(resource.WithHTTPClient(srv.Client()))
		res := mavenResource(&v1.Maven{RepoURL: srv.URL, GroupID: "com.example", ArtifactID: "lib", Version: "1.2.3"})
		_, err := r.DownloadResource(context.Background(), res, nil)
		require.NoError(t, err)
	})

	t.Run("checksum mismatch fails", func(t *testing.T) {
		srv := newSrv("0000000000000000000000000000000000000000", http.StatusOK)
		t.Cleanup(srv.Close)
		r := resource.NewResourceRepository(resource.WithHTTPClient(srv.Client()))
		res := mavenResource(&v1.Maven{RepoURL: srv.URL, GroupID: "com.example", ArtifactID: "lib", Version: "1.2.3"})
		_, err := r.DownloadResource(context.Background(), res, nil)
		require.ErrorContains(t, err, "mismatch")
	})

	t.Run("missing checksum is soft by default", func(t *testing.T) {
		srv := newSrv("", http.StatusNotFound)
		t.Cleanup(srv.Close)
		r := resource.NewResourceRepository(resource.WithHTTPClient(srv.Client()))
		res := mavenResource(&v1.Maven{RepoURL: srv.URL, GroupID: "com.example", ArtifactID: "lib", Version: "1.2.3"})
		_, err := r.DownloadResource(context.Background(), res, nil)
		require.NoError(t, err)
	})

	t.Run("missing checksum fails with hard verify", func(t *testing.T) {
		srv := newSrv("", http.StatusNotFound)
		t.Cleanup(srv.Close)
		r := resource.NewResourceRepository(resource.WithHTTPClient(srv.Client()), resource.WithHardVerify())
		res := mavenResource(&v1.Maven{RepoURL: srv.URL, GroupID: "com.example", ArtifactID: "lib", Version: "1.2.3"})
		_, err := r.DownloadResource(context.Background(), res, nil)
		require.Error(t, err)
	})

	t.Run("non-404 checksum status fails even in soft mode", func(t *testing.T) {
		srv := newSrv("", http.StatusForbidden)
		t.Cleanup(srv.Close)
		r := resource.NewResourceRepository(resource.WithHTTPClient(srv.Client()))
		res := mavenResource(&v1.Maven{RepoURL: srv.URL, GroupID: "com.example", ArtifactID: "lib", Version: "1.2.3"})
		_, err := r.DownloadResource(context.Background(), res, nil)
		require.ErrorContains(t, err, "403")
	})
}

func TestUploadResourceVerifyUpload(t *testing.T) {
	jar := "/com/example/lib/1.2.3/lib-1.2.3.jar"

	t.Run("re-download matches", func(t *testing.T) {
		stored := map[string][]byte{}
		var mu sync.Mutex
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			mu.Lock()
			defer mu.Unlock()
			switch req.Method {
			case http.MethodPut:
				b, _ := io.ReadAll(req.Body)
				stored[req.URL.Path] = b
				w.WriteHeader(http.StatusCreated)
			case http.MethodGet:
				b, ok := stored[req.URL.Path]
				if !ok {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(b)
			}
		}))
		t.Cleanup(srv.Close)

		r := resource.NewResourceRepository(resource.WithHTTPClient(srv.Client()), resource.WithVerifyUpload())
		res := mavenResource(&v1.Maven{RepoURL: srv.URL, GroupID: "com.example", ArtifactID: "lib", Version: "1.2.3"})
		_, err := r.UploadResource(context.Background(), res, inmemory.New(bytes.NewReader([]byte("JARDATA"))), nil)
		require.NoError(t, err)
	})

	t.Run("re-download mismatch fails", func(t *testing.T) {
		var mu sync.Mutex
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			mu.Lock()
			defer mu.Unlock()
			switch req.Method {
			case http.MethodPut:
				w.WriteHeader(http.StatusCreated)
			case http.MethodGet:
				if req.URL.Path == jar {
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("CORRUPTED")) // server returns different bytes
					return
				}
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		t.Cleanup(srv.Close)

		r := resource.NewResourceRepository(resource.WithHTTPClient(srv.Client()), resource.WithVerifyUpload())
		res := mavenResource(&v1.Maven{RepoURL: srv.URL, GroupID: "com.example", ArtifactID: "lib", Version: "1.2.3"})
		_, err := r.UploadResource(context.Background(), res, inmemory.New(bytes.NewReader([]byte("JARDATA"))), nil)
		require.ErrorContains(t, err, "mismatch")
	})
}

func TestDownloadResource_SnapshotMultiFile_Tgz(t *testing.T) {
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
			http.NotFound(w, req) // .sha1 siblings 404 -> soft pass
		}
	}))
	defer srv.Close()

	repo := resource.NewResourceRepository(resource.WithHTTPClient(srv.Client()))
	res := &descriptor.Resource{Access: &v1.Maven{
		Type: runtime.NewVersionedType(v1.Type, v1.Version), RepoURL: srv.URL + "/maven2",
		GroupID: "com.example", ArtifactID: "lib", Version: "1.0-SNAPSHOT", Extension: ptr("jar"),
	}}
	b, err := repo.DownloadResource(context.Background(), res, nil)
	if err != nil {
		t.Fatal(err)
	}

	// assert the blob is tagged as a flat tgz
	mt, ok := b.(interface{ MediaType() (string, bool) }).MediaType()
	require.True(t, ok)
	assert.Equal(t, "application/x-tgz", mt)

	// assert the archive holds both entries with the expected content
	entries := readTgzEntries(t, b)
	require.Len(t, entries, 2)
	assert.Equal(t, jar, entries["lib-1.0-20240101.120000-3.jar"])
	assert.Equal(t, src, entries["lib-1.0-20240101.120000-3-sources.jar"])
}

func TestDownloadResource_SnapshotPatternSingleMatch_PlainBlob(t *testing.T) {
	jar := []byte("JARDATA")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/maven2/com/example/lib/1.0-SNAPSHOT/maven-metadata.xml":
			// Only one snapshotVersion entry, and it matches the selector below,
			// so pattern resolution yields exactly one file.
			_, _ = w.Write([]byte(`<metadata><versioning><snapshotVersions>` +
				`<snapshotVersion><classifier>sources</classifier><extension>jar</extension><value>1.0-20240101.120000-3</value></snapshotVersion>` +
				`</snapshotVersions></versioning></metadata>`))
		case "/maven2/com/example/lib/1.0-SNAPSHOT/lib-1.0-20240101.120000-3-sources.jar":
			_, _ = w.Write(jar)
		default:
			http.NotFound(w, req) // .sha1 sibling 404 -> soft pass
		}
	}))
	defer srv.Close()

	repo := resource.NewResourceRepository(resource.WithHTTPClient(srv.Client()))
	res := mavenResource(&v1.Maven{
		RepoURL: srv.URL + "/maven2", GroupID: "com.example", ArtifactID: "lib", Version: "1.0-SNAPSHOT",
		Classifier: ptr("sources"),
	})
	b, err := repo.DownloadResource(context.Background(), res, nil)
	require.NoError(t, err)

	rc, err := b.ReadCloser()
	require.NoError(t, err)
	defer rc.Close()
	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, jar, data)

	// A single-file pattern match must be returned as a plain blob (not a tgz).
	mt, ok := b.(interface{ MediaType() (string, bool) }).MediaType()
	require.True(t, ok)
	assert.NotEqual(t, "application/x-tgz", mt)
	assert.Equal(t, "application/java-archive", mt)
}

// readTgzEntries reads a gzip-compressed tar blob and returns a map of entry
// name to its raw content.
func readTgzEntries(t *testing.T, b blob.ReadOnlyBlob) map[string][]byte {
	t.Helper()
	rc, err := b.ReadCloser()
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	gz, err := gzip.NewReader(rc)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	entries := map[string][]byte{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		entries[h.Name] = data
	}
	return entries
}

func ptr[T any](v T) *T { return &v }
