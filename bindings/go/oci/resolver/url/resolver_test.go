package url_test

import (
	"context"
	"encoding/base64"
	"log/slog"
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"

	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/oci/cache"
	"ocm.software/open-component-model/bindings/go/oci/internal/remotestore"
	"ocm.software/open-component-model/bindings/go/oci/resolver/url"
	"ocm.software/open-component-model/bindings/go/oci/spec"
)

// Custom transport to verify the custom client is being used
type customRoundTripper struct {
	transport   http.RoundTripper
	onRoundTrip func()
}

func (c *customRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if c.onRoundTrip != nil {
		c.onRoundTrip()
	}
	return c.transport.RoundTrip(req)
}

func TestNewURLPathResolver(t *testing.T) {
	baseURL := "http://example.com"
	resolver, err := url.New(url.WithBaseURL(baseURL))
	require.NoError(t, err)
	assert.NotNil(t, resolver)
}

func TestURLPathResolver_SetClient(t *testing.T) {
	resolver, err := url.New(url.WithBaseURL("http://example.com"))
	require.NoError(t, err)
	repo, err := remote.NewRepository("example.com/test")
	require.NoError(t, err)

	// Set the client
	resolver.SetClient(repo.Client)

	// Verify the client was set by using it
	store, err := resolver.StoreForReference(context.Background(), "example.com/test")
	require.NoError(t, err)
	assert.NotNil(t, store)
}

func TestURLPathResolver_ComponentVersionReference(t *testing.T) {
	resolver, err := url.New(url.WithBaseURL("http://example.com"))
	require.NoError(t, err)
	component := "ocm.software/test-component"
	version := "v1.0.0"
	expected := "http://example.com/component-descriptors/ocm.software/test-component:v1.0.0"
	result := resolver.ComponentVersionReference(t.Context(), component, version)
	assert.Equal(t, expected, result)
}

func TestURLPathResolver_ComponentVersionReferenceWithSubPath(t *testing.T) {
	tests := []struct {
		name      string
		baseURL   string
		subPath   string
		component string
		version   string
		expected  string
	}{
		{
			name:      "with subPath",
			baseURL:   "http://example.com",
			subPath:   "my-org/components",
			component: "ocm.software/test-component",
			version:   "v1.0.0",
			expected:  "http://example.com/my-org/components/component-descriptors/ocm.software/test-component:v1.0.0",
		},
		{
			name:      "without subPath",
			baseURL:   "http://example.com",
			subPath:   "",
			component: "ocm.software/test-component",
			version:   "v1.0.0",
			expected:  "http://example.com/component-descriptors/ocm.software/test-component:v1.0.0",
		},
		{
			name:      "with nested subPath",
			baseURL:   "http://example.com",
			subPath:   "org/team/project",
			component: "ocm.software/test-component",
			version:   "v2.1.0",
			expected:  "http://example.com/org/team/project/component-descriptors/ocm.software/test-component:v2.1.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := []url.Option{url.WithBaseURL(tt.baseURL)}
			if tt.subPath != "" {
				opts = append(opts, url.WithSubPath(tt.subPath))
			}
			resolver, err := url.New(opts...)
			require.NoError(t, err)
			result := resolver.ComponentVersionReference(t.Context(), tt.component, tt.version)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestURLPathResolver_BasePath(t *testing.T) {
	tests := []struct {
		name     string
		baseURL  string
		subPath  string
		expected string
	}{
		{
			name:     "without subPath",
			baseURL:  "http://example.com",
			subPath:  "",
			expected: "http://example.com/component-descriptors",
		},
		{
			name:     "with subPath",
			baseURL:  "http://example.com",
			subPath:  "my-org/components",
			expected: "http://example.com/my-org/components/component-descriptors",
		},
		{
			name:     "with nested subPath",
			baseURL:  "registry.example.com:5000",
			subPath:  "org/team/project",
			expected: "registry.example.com:5000/org/team/project/component-descriptors",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := []url.Option{url.WithBaseURL(tt.baseURL)}
			if tt.subPath != "" {
				opts = append(opts, url.WithSubPath(tt.subPath))
			}
			resolver, err := url.New(opts...)
			require.NoError(t, err)
			result := resolver.BasePath()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestURLPathResolver_StoreForReference(t *testing.T) {
	tests := []struct {
		name        string
		reference   string
		expectError bool
	}{
		{
			name:        "valid reference",
			reference:   "example.com/test-component:v1.0.0",
			expectError: false,
		},
		{
			name:        "invalid reference",
			reference:   "!invalid:reference",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver, err := url.New(url.WithBaseURL("http://example.com"))
			require.NoError(t, err)
			store, err := resolver.StoreForReference(context.Background(), tt.reference)

			if tt.expectError {
				require.Error(t, err)
				assert.Nil(t, store)
				return
			}

			require.NoError(t, err)
			assert.NotNil(t, store)
		})
	}
}

func TestURLPathResolver_Ping(t *testing.T) {
	ctx := context.Background()

	t.Run("ping with invalid URL fails", func(t *testing.T) {
		resolver, err := url.New(url.WithBaseURL("http://invalid.nonexistent.domain"))
		require.NoError(t, err)

		err = resolver.Ping(ctx)
		require.Error(t, err)
		// Should fail with ping error containing the domain
		assert.Contains(t, err.Error(), "failed to ping registry")
	})

	t.Run("ping with malformed URL fails", func(t *testing.T) {
		resolver, err := url.New(url.WithBaseURL("not-a-valid-url"))
		require.NoError(t, err)

		err = resolver.Ping(ctx)
		require.Error(t, err)
		// Should fail with ping error containing the URL
		assert.Contains(t, err.Error(), "failed to ping registry")
	})

	t.Run("ping uses configured base client", func(t *testing.T) {
		transportUsed := false

		// Create a test server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v2/" {
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		serverHost := server.URL[7:]
		resolver, err := url.New(url.WithBaseURL(serverHost), url.WithPlainHTTP(true))
		require.NoError(t, err)

		customTransport := &customRoundTripper{
			transport: http.DefaultTransport,
			onRoundTrip: func() {
				transportUsed = true
			},
		}

		// Create a custom client with the tracking transport
		customClient := &http.Client{
			Transport: customTransport,
		}
		resolver.SetClient(customClient)

		err = resolver.Ping(ctx)
		require.NoError(t, err)
		assert.True(t, transportUsed, "Expected custom transport to be used")

		transportUsed = false
		customClient = &http.Client{}
		resolver.SetClient(customClient)
		err = resolver.Ping(ctx)
		require.NoError(t, err)
		assert.False(t, transportUsed, "Expected custom transport to be NOT used")
	})

	t.Run("200 OK succeeds", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Equal(t, "/v2/", r.URL.Path)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		resolver, err := url.New(url.WithBaseURL(server.URL[7:]), url.WithPlainHTTP(true))
		require.NoError(t, err)

		err = resolver.Ping(ctx)
		assert.NoError(t, err)
	})

	t.Run("401 status should pass", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Equal(t, "/v2/", r.URL.Path)
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer server.Close()

		resolver, err := url.New(url.WithBaseURL(server.URL[7:]), url.WithPlainHTTP(true))
		require.NoError(t, err)

		err = resolver.Ping(ctx)
		assert.NoError(t, err)
	})

	t.Run("403 status should pass", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Equal(t, "/v2/", r.URL.Path)
			w.WriteHeader(http.StatusForbidden)
		}))
		defer server.Close()

		resolver, err := url.New(url.WithBaseURL(server.URL[7:]), url.WithPlainHTTP(true))
		require.NoError(t, err)

		err = resolver.Ping(ctx)
		assert.NoError(t, err)
	})

	t.Run("500 status should fail", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Equal(t, "/v2/", r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		resolver, err := url.New(url.WithBaseURL(server.URL[7:]), url.WithPlainHTTP(true))
		require.NoError(t, err)

		err = resolver.Ping(ctx)
		assert.Error(t, err)
	})

	t.Run("ping with baseURL containing path extracts hostname", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			// When baseURL contains a path but subPath is empty,
			// Ping should extract hostname and ping /v2/ on registry root
			assert.Equal(t, "/v2/", r.URL.Path)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		// baseURL with path, no subPath - should extract hostname
		serverHost := server.URL[7:] + "/registry-path"
		resolver, err := url.New(url.WithBaseURL(serverHost), url.WithPlainHTTP(true))
		require.NoError(t, err)

		err = resolver.Ping(ctx)
		assert.NoError(t, err)
	})

	t.Run("ping with subPath still extracts hostname from baseURL", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			// Even when subPath is set, Ping extracts hostname from baseURL
			// subPath is ignored for health checks
			assert.Equal(t, "/v2/", r.URL.Path)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		// Both baseURL and subPath set - still extracts hostname
		serverHost := server.URL[7:]
		resolver, err := url.New(
			url.WithBaseURL(serverHost),
			url.WithSubPath("my-org/my-repo"),
			url.WithPlainHTTP(true),
		)
		require.NoError(t, err)

		err = resolver.Ping(ctx)
		assert.NoError(t, err)
	})

	t.Run("ping with https scheme", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Equal(t, "/v2/", r.URL.Path)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		serverHost := server.URL[8:] // Remove "https://"
		resolver, err := url.New(url.WithBaseURL(serverHost))
		require.NoError(t, err)

		// Use the test server's client which trusts the test certificate
		resolver.SetClient(server.Client())

		err = resolver.Ping(ctx)
		assert.NoError(t, err)
	})

	t.Run("ping with basic authentication", func(t *testing.T) {
		authCalled := false
		expectedUsername := "testuser"
		expectedPassword := "testpass"

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Equal(t, "/v2/", r.URL.Path)

			auth := r.Header.Get("Authorization")
			if auth == "" {
				w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}

			// Verify basic auth credentials
			if auth[:6] == "Basic " {
				decoded, err := base64.StdEncoding.DecodeString(auth[6:])
				assert.NoError(t, err)
				assert.Equal(t, expectedUsername+":"+expectedPassword, string(decoded))
				authCalled = true
			}

			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		serverHost := server.URL[7:]
		resolver, err := url.New(url.WithBaseURL(serverHost), url.WithPlainHTTP(true))
		require.NoError(t, err)

		// Create auth client with credentials
		authClient := &auth.Client{
			Client: http.DefaultClient,
			Credential: func(ctx context.Context, registry string) (auth.Credential, error) {
				return auth.Credential{
					Username: expectedUsername,
					Password: expectedPassword,
				}, nil
			},
		}
		resolver.SetClient(authClient)

		err = resolver.Ping(ctx)
		require.NoError(t, err)
		assert.True(t, authCalled, "Expected authentication to be used")
	})

	t.Run("ping with bearer token authentication", func(t *testing.T) {
		tokenFetched := false
		authUsed := false
		expectedToken := "test-bearer-token"

		// Token server
		tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/token", r.URL.Path)
			tokenFetched = true
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, err := w.Write([]byte(`{"token":"` + expectedToken + `","access_token":"` + expectedToken + `"}`))
			assert.NoError(t, err)
		}))
		defer tokenServer.Close()

		// Registry server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Equal(t, "/v2/", r.URL.Path)

			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				w.Header().Set("WWW-Authenticate", `Bearer realm="`+tokenServer.URL+`/token",service="registry",scope="repository:test:pull"`)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}

			// Verify bearer token
			assert.Equal(t, "Bearer "+expectedToken, authHeader)
			authUsed = true
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		serverHost := server.URL[7:]
		resolver, err := url.New(url.WithBaseURL(serverHost), url.WithPlainHTTP(true))
		require.NoError(t, err)

		// Create auth client with credentials
		authClient := &auth.Client{
			Client: http.DefaultClient,
			Credential: func(ctx context.Context, registry string) (auth.Credential, error) {
				return auth.Credential{
					Username: "testuser",
					Password: "testpass",
				}, nil
			},
		}
		resolver.SetClient(authClient)

		err = resolver.Ping(ctx)
		require.NoError(t, err)
		assert.True(t, tokenFetched, "Expected token to be fetched from auth server")
		assert.True(t, authUsed, "Expected bearer token to be used in request")
	})
}

func TestURLPathResolver_DynamicCacheSwapping(t *testing.T) {
	ctx := context.Background()
	resolver, err := url.New(url.WithBaseURL("http://example.com"))
	require.NoError(t, err)

	ref := "example.com/test-component:v1.0.0"

	// 1. Initially, no caches are set. StoreForReference should return the
	// base *remotestore.RemoteStore (Wrap's passthrough path).
	store, err := resolver.StoreForReference(ctx, ref)
	require.NoError(t, err)
	assert.IsType(t, &remotestore.RemoteStore{}, store)

	// Enabling blob caching must preserve chunked and streaming pushes.
	dummyBlobCache := &cache.BlobCache{}
	resolver.SetBlobCache(dummyBlobCache)

	store, err = resolver.StoreForReference(ctx, ref)
	require.NoError(t, err)
	assert.Implements(t, (*spec.Store)(nil), store)
	assert.Implements(t, (*remotestore.StreamingPusher)(nil), store)
	_, isPlain := store.(*remotestore.RemoteStore)
	assert.False(t, isPlain, "configured blob cache must wrap the backing store")
	wrapped, ok := store.(*cache.CachedStore)
	require.True(t, ok)
	assert.Equal(t, dummyBlobCache, wrapped.BlobCache)
	assert.Nil(t, wrapped.ReferenceCache)

	// 3. Set reference cache too.
	dummyRefCache := &cache.ReferenceCache{}
	resolver.SetReferenceCache(dummyRefCache)

	store, err = resolver.StoreForReference(ctx, ref)
	require.NoError(t, err)
	assert.Implements(t, (*spec.Store)(nil), store)
	assert.Implements(t, (*remotestore.StreamingPusher)(nil), store)
	wrapped, ok = store.(*cache.CachedStore)
	require.True(t, ok)
	assert.Equal(t, dummyBlobCache, wrapped.BlobCache)
	assert.Equal(t, dummyRefCache, wrapped.ReferenceCache)

	// 4. Set both back to nil. StoreForReference should return the base
	// *remotestore.RemoteStore again.
	resolver.SetBlobCache(nil)
	resolver.SetReferenceCache(nil)

	store, err = resolver.StoreForReference(ctx, ref)
	require.NoError(t, err)
	assert.IsType(t, &remotestore.RemoteStore{}, store)
}

func TestURLPathResolver_PlainHTTPFromHTTPConfig(t *testing.T) {
	tr, fa := true, false
	hostCfg := func(plain *bool) *httpv1alpha1.HostConfig {
		return &httpv1alpha1.HostConfig{ProtocolConfig: httpv1alpha1.ProtocolConfig{PlainHTTP: plain}}
	}

	tests := []struct {
		name      string
		cfg       *httpv1alpha1.Config
		opts      []url.Option
		reference string
		plainHTTP bool
		wantErr   string
	}{
		{
			name:      "no config keeps HTTPS",
			reference: "registry.local:5000/repo:v1",
		},
		{
			name:      "per-host true turns on plain HTTP",
			cfg:       &httpv1alpha1.Config{Hosts: map[string]*httpv1alpha1.HostConfig{"registry.local:5000": hostCfg(&tr)}},
			reference: "registry.local:5000/repo:v1",
			plainHTTP: true,
		},
		{
			name:      "entry for another host does not apply",
			cfg:       &httpv1alpha1.Config{Hosts: map[string]*httpv1alpha1.HostConfig{"registry.local": hostCfg(&tr)}},
			reference: "ghcr.io/repo:v1",
		},
		{
			name:      "global true applies to every host",
			cfg:       &httpv1alpha1.Config{ProtocolConfig: httpv1alpha1.ProtocolConfig{PlainHTTP: &tr}},
			reference: "ghcr.io/repo:v1",
			plainHTTP: true,
		},
		{
			name:      "WithPlainHTTP without config uses cleartext",
			opts:      []url.Option{url.WithPlainHTTP(true)},
			reference: "registry.local/repo:v1",
			plainHTTP: true,
		},
		{
			name:      "WithPlainHTTP with per-host true uses cleartext",
			cfg:       &httpv1alpha1.Config{Hosts: map[string]*httpv1alpha1.HostConfig{"registry.local": hostCfg(&tr)}},
			opts:      []url.Option{url.WithPlainHTTP(true)},
			reference: "registry.local/repo:v1",
			plainHTTP: true,
		},
		{
			name:      "WithPlainHTTP against per-host false is an error",
			cfg:       &httpv1alpha1.Config{Hosts: map[string]*httpv1alpha1.HostConfig{"registry.local": hostCfg(&fa)}},
			opts:      []url.Option{url.WithPlainHTTP(true)},
			reference: "registry.local/repo:v1",
			wantErr:   `conflicts with plainHTTP: false`,
		},
		{
			name:      "http reference without config uses cleartext",
			reference: "http://registry.local:5000/repo:v1",
			plainHTTP: true,
		},
		{
			name:      "http reference with per-host true uses cleartext",
			cfg:       &httpv1alpha1.Config{Hosts: map[string]*httpv1alpha1.HostConfig{"registry.local:5000": hostCfg(&tr)}},
			reference: "http://registry.local:5000/repo:v1",
			plainHTTP: true,
		},
		{
			name:      "http reference against per-host false is an error",
			cfg:       &httpv1alpha1.Config{Hosts: map[string]*httpv1alpha1.HostConfig{"registry.local:5000": hostCfg(&fa)}},
			reference: "http://registry.local:5000/repo:v1",
			wantErr:   `host "registry.local:5000"`,
		},
		{
			name:      "http reference against global false is an error",
			cfg:       &httpv1alpha1.Config{ProtocolConfig: httpv1alpha1.ProtocolConfig{PlainHTTP: &fa}},
			reference: "http://registry.local:5000/repo:v1",
			wantErr:   `conflicts with plainHTTP: false`,
		},
		{
			name: "http reference with per-host true under global false uses cleartext",
			cfg: &httpv1alpha1.Config{
				ProtocolConfig: httpv1alpha1.ProtocolConfig{PlainHTTP: &fa},
				Hosts:          map[string]*httpv1alpha1.HostConfig{"registry.local:5000": hostCfg(&tr)},
			},
			reference: "http://registry.local:5000/repo:v1",
			plainHTTP: true,
		},
		{
			name:      "https reference follows per-host true",
			cfg:       &httpv1alpha1.Config{Hosts: map[string]*httpv1alpha1.HostConfig{"registry.local:5000": hostCfg(&tr)}},
			reference: "https://registry.local:5000/repo:v1",
			plainHTTP: true,
		},
		{
			name:      "https reference follows global true",
			cfg:       &httpv1alpha1.Config{ProtocolConfig: httpv1alpha1.ProtocolConfig{PlainHTTP: &tr}},
			reference: "https://registry.local:5000/repo:v1",
			plainHTTP: true,
		},
		{
			name: "docker.io reference matches the registry-1.docker.io entry set to false",
			cfg: &httpv1alpha1.Config{
				ProtocolConfig: httpv1alpha1.ProtocolConfig{PlainHTTP: &tr},
				Hosts:          map[string]*httpv1alpha1.HostConfig{"registry-1.docker.io": hostCfg(&fa)},
			},
			reference: "docker.io/library/alpine:3",
		},
		{
			name:      "docker.io reference matches the registry-1.docker.io entry set to true",
			cfg:       &httpv1alpha1.Config{Hosts: map[string]*httpv1alpha1.HostConfig{"registry-1.docker.io": hostCfg(&tr)}},
			reference: "docker.io/library/alpine:3",
			plainHTTP: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			opts := append([]url.Option{url.WithBaseURL("registry.local"), url.WithHTTPConfig(tt.cfg)}, tt.opts...)
			resolver, err := url.New(opts...)
			r.NoError(err)

			store, err := resolver.StoreForReference(t.Context(), tt.reference)
			if tt.wantErr != "" {
				r.ErrorContains(err, tt.wantErr)
				r.ErrorContains(err, tt.reference, "the error must name the reference")
				r.Nil(store)
				return
			}
			r.NoError(err)
			remoteStore, ok := store.(*remotestore.RemoteStore)
			r.True(ok, "expected a bare RemoteStore when no cache is configured, got %T", store)
			r.Equal(tt.plainHTTP, remoteStore.PlainHTTP)
		})
	}

	t.Run("ping rejects an http base URL against per-host false", func(t *testing.T) {
		r := require.New(t)
		cfg := &httpv1alpha1.Config{Hosts: map[string]*httpv1alpha1.HostConfig{"registry.local:5000": hostCfg(&fa)}}
		resolver, err := url.New(url.WithBaseURL("registry.local:5000"), url.WithPlainHTTP(true), url.WithHTTPConfig(cfg))
		r.NoError(err)
		client := &recordingClient{}
		resolver.SetClient(client)

		r.ErrorContains(resolver.Ping(t.Context()), `host "registry.local:5000"`)
		r.Nil(client.url, "the ping must not reach the registry")
	})

	t.Run("ping honours per-host plain HTTP", func(t *testing.T) {
		r := require.New(t)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.URL.Path == "/v2/" {
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()
		serverHost := server.URL[len("http://"):]

		cfg := &httpv1alpha1.Config{Hosts: map[string]*httpv1alpha1.HostConfig{serverHost: hostCfg(&tr)}}
		resolver, err := url.New(url.WithBaseURL(serverHost), url.WithHTTPConfig(cfg))
		r.NoError(err)
		r.NoError(resolver.Ping(t.Context()))

		resolver, err = url.New(url.WithBaseURL(serverHost))
		r.NoError(err)
		r.ErrorContains(resolver.Ping(t.Context()), "server gave HTTP response to HTTPS client", "without the host entry the ping must try HTTPS against a plain HTTP server")
	})

	t.Run("ping keys the host lookup on the request host", func(t *testing.T) {
		r := require.New(t)
		cfg := &httpv1alpha1.Config{
			ProtocolConfig: httpv1alpha1.ProtocolConfig{PlainHTTP: &tr},
			Hosts:          map[string]*httpv1alpha1.HostConfig{"registry-1.docker.io": hostCfg(&fa)},
		}
		resolver, err := url.New(url.WithBaseURL("docker.io"), url.WithHTTPConfig(cfg))
		r.NoError(err)
		client := &recordingClient{}
		resolver.SetClient(client)

		r.NoError(resolver.Ping(t.Context()))
		r.NotNil(client.url, "the ping must reach the client")
		r.Equal("registry-1.docker.io", client.url.Host)
		r.Equal("https", client.url.Scheme, "the registry-1.docker.io entry must override global plainHTTP")
	})

	t.Run("ping follows global plain HTTP over an https base URL", func(t *testing.T) {
		r := require.New(t)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.URL.Path == "/v2/" {
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()
		baseURL := "https://" + server.URL[len("http://"):]

		cfg := &httpv1alpha1.Config{ProtocolConfig: httpv1alpha1.ProtocolConfig{PlainHTTP: &tr}}
		resolver, err := url.New(url.WithBaseURL(baseURL), url.WithHTTPConfig(cfg))
		r.NoError(err)
		r.NoError(resolver.Ping(t.Context()), "global plainHTTP wins over an https:// base URL")

		resolver, err = url.New(url.WithBaseURL(baseURL))
		r.NoError(err)
		r.ErrorContains(resolver.Ping(t.Context()), "server gave HTTP response to HTTPS client", "without the config the ping must use the https:// scheme of the base URL")
	})
}

// recordingClient answers every request with 200 OK without network access
// and keeps the URL of the last request.
type recordingClient struct {
	url *neturl.URL
}

func (c *recordingClient) Do(req *http.Request) (*http.Response, error) {
	c.url = req.URL
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: req}, nil
}

type warnCapture struct {
	records []slog.Record
}

func (c *warnCapture) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelWarn
}

func (c *warnCapture) Handle(_ context.Context, r slog.Record) error {
	c.records = append(c.records, r)
	return nil
}

func (c *warnCapture) WithAttrs(_ []slog.Attr) slog.Handler { return c }
func (c *warnCapture) WithGroup(_ string) slog.Handler      { return c }

// hosts returns the distinct host attributes of the records, in first-seen
// order, "" for a record without one. How often a host is reported is not
// part of the contract, only that it is.
func (c *warnCapture) hosts() []string {
	hosts := []string{}
	for _, rec := range c.records {
		host := ""
		rec.Attrs(func(a slog.Attr) bool {
			if a.Key == "host" {
				host = a.Value.String()
			}
			return true
		})
		if !slices.Contains(hosts, host) {
			hosts = append(hosts, host)
		}
	}
	return hosts
}

func TestURLPathResolver_PlainHTTPWarning(t *testing.T) {
	tr, fa := true, false
	hostCfg := func(plain *bool) *httpv1alpha1.HostConfig {
		return &httpv1alpha1.HostConfig{ProtocolConfig: httpv1alpha1.ProtocolConfig{PlainHTTP: plain}}
	}

	tests := []struct {
		name  string
		cfg   *httpv1alpha1.Config
		hosts []string
	}{
		{
			name:  "nil config warns nothing",
			hosts: []string{},
		},
		{
			name:  "global true warns for each host used",
			cfg:   &httpv1alpha1.Config{ProtocolConfig: httpv1alpha1.ProtocolConfig{PlainHTTP: &tr}},
			hosts: []string{"registry.local:5000", "ghcr.io"},
		},
		{
			name:  "per-host true warns for that host only",
			cfg:   &httpv1alpha1.Config{Hosts: map[string]*httpv1alpha1.HostConfig{"registry.local:5000": hostCfg(&tr)}},
			hosts: []string{"registry.local:5000"},
		},
		{
			name: "a host entry that is never used is not reported",
			cfg: &httpv1alpha1.Config{Hosts: map[string]*httpv1alpha1.HostConfig{
				"registry.local:5000": hostCfg(&tr),
				"unused.local":        hostCfg(&tr),
			}},
			hosts: []string{"registry.local:5000"},
		},
		{
			name: "per-host false under global true is not reported",
			cfg: &httpv1alpha1.Config{
				ProtocolConfig: httpv1alpha1.ProtocolConfig{PlainHTTP: &tr},
				Hosts:          map[string]*httpv1alpha1.HostConfig{"ghcr.io": hostCfg(&fa)},
			},
			hosts: []string{"registry.local:5000"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			capture := &warnCapture{}
			orig := slog.Default()
			slog.SetDefault(slog.New(capture))
			t.Cleanup(func() { slog.SetDefault(orig) })

			resolver, err := url.New(url.WithBaseURL("registry.local"), url.WithHTTPConfig(tt.cfg))
			r.NoError(err)
			r.Empty(capture.records, "creating the resolver must not warn")

			for _, ref := range []string{"registry.local:5000/a:v1", "ghcr.io/b:v1"} {
				_, err := resolver.StoreForReference(t.Context(), ref)
				r.NoError(err)
			}
			r.Equal(tt.hosts, capture.hosts())
		})
	}
}

// deprecations returns the distinct host attributes of the records that link
// the http:// prefix migration guide, in first-seen order.
func (c *warnCapture) deprecations() []string {
	hosts := []string{}
	for _, rec := range c.records {
		host, guide := "", ""
		rec.Attrs(func(a slog.Attr) bool {
			switch a.Key {
			case "host":
				host = a.Value.String()
			case "url":
				guide = a.Value.String()
			}
			return true
		})
		if guide == "https://ocm.software/docs/guides/transfer/configure-http/#migrate-from-http-prefixes" && !slices.Contains(hosts, host) {
			hosts = append(hosts, host)
		}
	}
	return hosts
}

func TestURLPathResolver_HTTPPrefixDeprecationWarning(t *testing.T) {
	tr := true
	hostCfg := func(plain *bool) *httpv1alpha1.HostConfig {
		return &httpv1alpha1.HostConfig{ProtocolConfig: httpv1alpha1.ProtocolConfig{PlainHTTP: plain}}
	}
	perHostTrue := &httpv1alpha1.Config{Hosts: map[string]*httpv1alpha1.HostConfig{"registry.local:5000": hostCfg(&tr)}}

	tests := []struct {
		name      string
		opts      []url.Option
		ping      bool
		reference string
		hosts     []string
	}{
		{
			name:      "http base URL without config warns",
			opts:      []url.Option{url.WithBaseURL("registry.local:5000"), url.WithPlainHTTP(true)},
			reference: "registry.local:5000/repo:v1",
			hosts:     []string{"registry.local:5000"},
		},
		{
			name:      "http base URL warns on ping",
			opts:      []url.Option{url.WithBaseURL("registry.local:5000"), url.WithPlainHTTP(true)},
			ping:      true,
			reference: "registry.local:5000/repo:v1",
			hosts:     []string{"registry.local:5000"},
		},
		{
			name:      "http base URL names the request host",
			opts:      []url.Option{url.WithBaseURL("docker.io"), url.WithPlainHTTP(true)},
			ping:      true,
			reference: "docker.io/library/alpine:3",
			hosts:     []string{"registry-1.docker.io"},
		},
		{
			name:      "http base URL with per-host plainHTTP still warns",
			opts:      []url.Option{url.WithBaseURL("registry.local:5000"), url.WithPlainHTTP(true), url.WithHTTPConfig(perHostTrue)},
			ping:      true,
			reference: "registry.local:5000/repo:v1",
			hosts:     []string{"registry.local:5000"},
		},
		{
			name:      "http reference without config warns",
			opts:      []url.Option{url.WithBaseURL("registry.local")},
			reference: "http://registry.local:5000/repo:v1",
			hosts:     []string{"registry.local:5000"},
		},
		{
			name:      "http reference with per-host plainHTTP still warns",
			opts:      []url.Option{url.WithBaseURL("registry.local"), url.WithHTTPConfig(perHostTrue)},
			reference: "http://registry.local:5000/repo:v1",
			hosts:     []string{"registry.local:5000"},
		},
		{
			name:      "scheme-less reference with per-host plainHTTP does not warn",
			opts:      []url.Option{url.WithBaseURL("registry.local"), url.WithHTTPConfig(perHostTrue)},
			reference: "registry.local:5000/repo:v1",
			hosts:     []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			capture := &warnCapture{}
			orig := slog.Default()
			slog.SetDefault(slog.New(capture))
			t.Cleanup(func() { slog.SetDefault(orig) })

			resolver, err := url.New(tt.opts...)
			r.NoError(err)
			resolver.SetClient(&recordingClient{})
			r.Empty(capture.records, "creating the resolver must not warn")

			if tt.ping {
				r.NoError(resolver.Ping(t.Context()))
			}
			_, err = resolver.StoreForReference(t.Context(), tt.reference)
			r.NoError(err)
			r.Equal(tt.hosts, capture.deprecations())
		})
	}

	t.Run("a rejected http prefix does not warn", func(t *testing.T) {
		r := require.New(t)
		capture := &warnCapture{}
		orig := slog.Default()
		slog.SetDefault(slog.New(capture))
		t.Cleanup(func() { slog.SetDefault(orig) })

		fa := false
		perHostFalse := &httpv1alpha1.Config{Hosts: map[string]*httpv1alpha1.HostConfig{"registry.local:5000": hostCfg(&fa)}}
		resolver, err := url.New(url.WithBaseURL("registry.local"), url.WithHTTPConfig(perHostFalse))
		r.NoError(err)

		_, err = resolver.StoreForReference(t.Context(), "http://registry.local:5000/repo:v1")
		r.ErrorContains(err, "plainHTTP: false")
		r.Empty(capture.deprecations(), "the error replaces the warning")
	})
}
