package resource_test

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	credv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
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

	// returned resource carries the maven access + media type
	var got v1.Maven
	require.NoError(t, mavenaccess.Scheme.Convert(updated.Access, &got))
	assert.Equal(t, "application/java-archive", got.MediaType)
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
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("JARDATA"))
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
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("JARDATA"))
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
