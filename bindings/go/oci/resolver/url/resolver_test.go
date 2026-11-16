package url_test

import (
	"context"
	"encoding/base64"
	"log/slog"
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"slices"
	"strings"
	"testing"

	"github.com/oras-project/oras-go/v3/registry/remote"
	"github.com/oras-project/oras-go/v3/registry/remote/auth"
	"github.com/oras-project/oras-go/v3/registry/remote/credentials"
	"github.com/oras-project/oras-go/v3/registry/remote/properties"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	resolver.SetClient(repo.Registry.Client)

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
		resolver, err := url.New(url.WithBaseURL("invalid.nonexistent.domain"))
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
			CredentialFunc: func(ctx context.Context, _ properties.Resource) (credentials.Credential, error) {
				return credentials.Credential{
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
			CredentialFunc: func(ctx context.Context, _ properties.Resource) (credentials.Credential, error) {
				return credentials.Credential{
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

// deprecated returns the distinct hosts of the records that link the scheme
// prefix migration guide, in first-seen order. How often a host is reported
// is not part of the contract, only that it is.
func (c *warnCapture) deprecated() []string {
	return c.hostsWhere(func(rec slog.Record, attrs map[string]string) bool {
		return attrs["url"] == "https://ocm.software/docs/changelog/migrate-from-http-prefixes/"
	})
}

// downgraded returns the distinct hosts of the records that warn about a
// cleartext connection, in first-seen order.
func (c *warnCapture) downgraded() []string {
	return c.hostsWhere(func(rec slog.Record, attrs map[string]string) bool {
		return strings.Contains(rec.Message, "downgraded to cleartext HTTP")
	})
}

func (c *warnCapture) hostsWhere(match func(rec slog.Record, attrs map[string]string) bool) []string {
	hosts := []string{}
	for _, rec := range c.records {
		attrs := map[string]string{}
		rec.Attrs(func(a slog.Attr) bool {
			attrs[a.Key] = a.Value.String()
			return true
		})
		if match(rec, attrs) && !slices.Contains(hosts, attrs["host"]) {
			hosts = append(hosts, attrs["host"])
		}
	}
	return hosts
}

func captureWarnings(t *testing.T) *warnCapture {
	t.Helper()
	capture := &warnCapture{}
	orig := slog.Default()
	slog.SetDefault(slog.New(capture))
	t.Cleanup(func() { slog.SetDefault(orig) })
	return capture
}

func hostCfg(plain *bool) *httpv1alpha1.HostConfig {
	return &httpv1alpha1.HostConfig{ProtocolConfig: httpv1alpha1.ProtocolConfig{PlainHTTP: plain}}
}

// TestURLPathResolver_PlainHTTP pins the decision table: the HTTP
// configuration decides the connection, a scheme prefix is deprecated input
// that is only checked against it.
//
//	plainHTTP | prefix   | connection | result
//	false     | https:// | https      | prefix deprecated
//	true      | https:// | http       | prefix deprecated + downgraded
//	true      | http://  | http       | prefix deprecated
//	false     | http://  | -          | error + prefix deprecated
//	false     | none     | https      | silent
//	true      | none     | http       | downgraded
//
// Every row runs with the prefix on the reference and with the prefix on the
// base URL (WithBaseURLScheme) and a scheme-less reference.
func TestURLPathResolver_PlainHTTP(t *testing.T) {
	const host = "registry.local:5000"

	tests := []struct {
		name           string
		plain          *bool // plainHTTP for the host in the HTTP configuration
		scheme         string
		wantPlain      bool
		wantErr        string
		wantDeprecated bool
		wantDowngraded bool
	}{
		{name: "unset with https:// keeps HTTPS and deprecates the prefix", scheme: "https", wantDeprecated: true},
		{name: "false with https:// keeps HTTPS and deprecates the prefix", plain: new(false), scheme: "https", wantDeprecated: true},
		{name: "true with https:// downgrades and deprecates the prefix", plain: new(true), scheme: "https", wantPlain: true, wantDeprecated: true, wantDowngraded: true},
		{name: "true with http:// uses HTTP and deprecates the prefix", plain: new(true), scheme: "http", wantPlain: true, wantDeprecated: true},
		{name: "unset with http:// is an error and deprecates the prefix", scheme: "http", wantErr: "plain HTTP is not enabled", wantDeprecated: true},
		{name: "false with http:// is an error and deprecates the prefix", plain: new(false), scheme: "http", wantErr: "plain HTTP is not enabled", wantDeprecated: true},
		{name: "unset without prefix keeps HTTPS silently"},
		{name: "false without prefix keeps HTTPS silently", plain: new(false)},
		{name: "true without prefix downgrades", plain: new(true), wantPlain: true, wantDowngraded: true},
	}

	for _, tt := range tests {
		var cfg *httpv1alpha1.Config
		if tt.plain != nil {
			cfg = &httpv1alpha1.Config{Hosts: map[string]*httpv1alpha1.HostConfig{host: hostCfg(tt.plain)}}
		}
		wantHosts := func(want bool) []string {
			if want {
				return []string{host}
			}
			return []string{}
		}
		check := func(t *testing.T, capture *warnCapture, store spec.Store, err error) {
			t.Helper()
			r := require.New(t)
			r.Equal(wantHosts(tt.wantDeprecated), capture.deprecated(), "prefix deprecation warning")
			r.Equal(wantHosts(tt.wantDowngraded), capture.downgraded(), "downgrade warning")
			if tt.wantErr != "" {
				r.ErrorContains(err, tt.wantErr)
				r.ErrorContains(err, host, "the error must name the host")
				r.Nil(store)
				return
			}
			r.NoError(err)
			remoteStore, ok := store.(*remotestore.RemoteStore)
			r.True(ok, "expected a bare RemoteStore when no cache is configured, got %T", store)
			r.Equal(tt.wantPlain, remoteStore.Registry.PlainHTTP)
		}

		t.Run(tt.name+" (prefix on the reference)", func(t *testing.T) {
			r := require.New(t)
			capture := captureWarnings(t)
			resolver, err := url.New(url.WithBaseURL("registry.local"), url.WithHTTPConfig(cfg))
			r.NoError(err)
			r.Empty(capture.records, "creating the resolver must not warn")

			reference := host + "/repo:v1"
			if tt.scheme != "" {
				reference = tt.scheme + "://" + reference
			}
			store, err := resolver.StoreForReference(t.Context(), reference)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, reference, "the error must name the reference")
			}
			check(t, capture, store, err)
		})

		t.Run(tt.name+" (prefix on the base URL)", func(t *testing.T) {
			r := require.New(t)
			capture := captureWarnings(t)
			resolver, err := url.New(url.WithBaseURL(host), url.WithBaseURLScheme(tt.scheme), url.WithHTTPConfig(cfg))
			r.NoError(err)
			r.Empty(capture.records, "creating the resolver must not warn")

			store, err := resolver.StoreForReference(t.Context(), host+"/repo:v1")
			check(t, capture, store, err)
		})
	}
}

// TestURLPathResolver_BaseSchemeOnlyForBaseHost pins that the scheme prefix
// of the base URL applies only to references on the base host. A resource on
// another registry (for example ghcr.io next to an http:// base) has no
// scheme of its own and must not inherit the base one.
func TestURLPathResolver_BaseSchemeOnlyForBaseHost(t *testing.T) {
	const baseHost = "base.local:5000"

	tests := []struct {
		name           string
		cfg            *httpv1alpha1.Config
		reference      string
		wantPlain      bool
		wantErr        string
		wantDeprecated []string
	}{
		{
			name:           "base host without config inherits http:// and fails",
			reference:      baseHost + "/repo:v1",
			wantErr:        "plain HTTP is not enabled",
			wantDeprecated: []string{baseHost},
		},
		{
			name:           "base host with plainHTTP inherits http://",
			cfg:            &httpv1alpha1.Config{Hosts: map[string]*httpv1alpha1.HostConfig{baseHost: hostCfg(new(true))}},
			reference:      baseHost + "/repo:v1",
			wantPlain:      true,
			wantDeprecated: []string{baseHost},
		},
		{
			name:           "other host without config keeps HTTPS silently",
			reference:      "other.host/repo:v1",
			wantDeprecated: []string{},
		},
		{
			name:           "other host with plainHTTP only for the base host keeps HTTPS silently",
			cfg:            &httpv1alpha1.Config{Hosts: map[string]*httpv1alpha1.HostConfig{baseHost: hostCfg(new(true))}},
			reference:      "other.host/repo:v1",
			wantDeprecated: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			capture := captureWarnings(t)
			resolver, err := url.New(url.WithBaseURL(baseHost), url.WithBaseURLScheme("http"), url.WithHTTPConfig(tt.cfg))
			r.NoError(err)

			store, err := resolver.StoreForReference(t.Context(), tt.reference)
			r.Equal(tt.wantDeprecated, capture.deprecated(), "prefix deprecation warning")
			if tt.wantErr != "" {
				r.ErrorContains(err, tt.wantErr)
				return
			}
			r.NoError(err)
			remoteStore, ok := store.(*remotestore.RemoteStore)
			r.True(ok, "expected a bare RemoteStore when no cache is configured, got %T", store)
			r.Equal(tt.wantPlain, remoteStore.Registry.PlainHTTP)
		})
	}
}

func TestURLPathResolver_PlainHTTPFromHTTPConfig(t *testing.T) {
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
			name:      "entry for another host does not apply",
			cfg:       &httpv1alpha1.Config{Hosts: map[string]*httpv1alpha1.HostConfig{"registry.local": hostCfg(new(true))}},
			reference: "ghcr.io/repo:v1",
		},
		{
			name:      "global true applies to every host",
			cfg:       &httpv1alpha1.Config{ProtocolConfig: httpv1alpha1.ProtocolConfig{PlainHTTP: new(true)}},
			reference: "ghcr.io/repo:v1",
			plainHTTP: true,
		},
		{
			name:      "global true allows an http reference",
			cfg:       &httpv1alpha1.Config{ProtocolConfig: httpv1alpha1.ProtocolConfig{PlainHTTP: new(true)}},
			reference: "http://registry.local:5000/repo:v1",
			plainHTTP: true,
		},
		{
			name:      "global false rejects an http reference",
			cfg:       &httpv1alpha1.Config{ProtocolConfig: httpv1alpha1.ProtocolConfig{PlainHTTP: new(false)}},
			reference: "http://registry.local:5000/repo:v1",
			wantErr:   `host "registry.local:5000"`,
		},
		{
			name: "per-host true under global false allows an http reference",
			cfg: &httpv1alpha1.Config{
				ProtocolConfig: httpv1alpha1.ProtocolConfig{PlainHTTP: new(false)},
				Hosts:          map[string]*httpv1alpha1.HostConfig{"registry.local:5000": hostCfg(new(true))},
			},
			reference: "http://registry.local:5000/repo:v1",
			plainHTTP: true,
		},
		{
			name: "per-host false under global true rejects an http reference",
			cfg: &httpv1alpha1.Config{
				ProtocolConfig: httpv1alpha1.ProtocolConfig{PlainHTTP: new(true)},
				Hosts:          map[string]*httpv1alpha1.HostConfig{"registry.local:5000": hostCfg(new(false))},
			},
			reference: "http://registry.local:5000/repo:v1",
			wantErr:   `host "registry.local:5000"`,
		},
		{
			name:      "WithPlainHTTP without config uses cleartext",
			opts:      []url.Option{url.WithPlainHTTP(true)},
			reference: "registry.local/repo:v1",
			plainHTTP: true,
		},
		{
			name:      "WithPlainHTTP allows an http reference",
			opts:      []url.Option{url.WithPlainHTTP(true)},
			reference: "http://registry.local/repo:v1",
			plainHTTP: true,
		},
		{
			name:      "per-host false wins over WithPlainHTTP",
			cfg:       &httpv1alpha1.Config{Hosts: map[string]*httpv1alpha1.HostConfig{"registry.local": hostCfg(new(false))}},
			opts:      []url.Option{url.WithPlainHTTP(true)},
			reference: "registry.local/repo:v1",
		},
		{
			name:      "per-host false wins over WithPlainHTTP and rejects an http reference",
			cfg:       &httpv1alpha1.Config{Hosts: map[string]*httpv1alpha1.HostConfig{"registry.local": hostCfg(new(false))}},
			opts:      []url.Option{url.WithPlainHTTP(true)},
			reference: "http://registry.local/repo:v1",
			wantErr:   `host "registry.local"`,
		},
		{
			name: "docker.io reference matches the registry-1.docker.io entry set to false",
			cfg: &httpv1alpha1.Config{
				ProtocolConfig: httpv1alpha1.ProtocolConfig{PlainHTTP: new(true)},
				Hosts:          map[string]*httpv1alpha1.HostConfig{"registry-1.docker.io": hostCfg(new(false))},
			},
			reference: "docker.io/library/alpine:3",
		},
		{
			name:      "docker.io reference matches the registry-1.docker.io entry set to true",
			cfg:       &httpv1alpha1.Config{Hosts: map[string]*httpv1alpha1.HostConfig{"registry-1.docker.io": hostCfg(new(true))}},
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
			r.Equal(tt.plainHTTP, remoteStore.Registry.PlainHTTP)
		})
	}

	t.Run("a cached store does not warn again", func(t *testing.T) {
		r := require.New(t)
		capture := captureWarnings(t)
		cfg := &httpv1alpha1.Config{Hosts: map[string]*httpv1alpha1.HostConfig{"registry.local:5000": hostCfg(new(true))}}
		resolver, err := url.New(url.WithBaseURL("registry.local"), url.WithHTTPConfig(cfg))
		r.NoError(err)

		for range 2 {
			_, err := resolver.StoreForReference(t.Context(), "registry.local:5000/repo:v1")
			r.NoError(err)
		}
		r.Len(capture.records, 1)
	})

	t.Run("warnings name each host used, not the entries configured", func(t *testing.T) {
		r := require.New(t)
		capture := captureWarnings(t)
		cfg := &httpv1alpha1.Config{
			ProtocolConfig: httpv1alpha1.ProtocolConfig{PlainHTTP: new(true)},
			Hosts: map[string]*httpv1alpha1.HostConfig{
				"ghcr.io":      hostCfg(new(false)),
				"unused.local": hostCfg(new(true)),
			},
		}
		resolver, err := url.New(url.WithBaseURL("registry.local"), url.WithHTTPConfig(cfg))
		r.NoError(err)

		for _, ref := range []string{"registry.local:5000/a:v1", "ghcr.io/b:v1", "quay.io/c:v1"} {
			_, err := resolver.StoreForReference(t.Context(), ref)
			r.NoError(err)
		}
		r.Equal([]string{"registry.local:5000", "quay.io"}, capture.downgraded())
		r.Empty(capture.deprecated())
	})
}

func TestURLPathResolver_Ping_PlainHTTP(t *testing.T) {
	t.Run("rejects an http base URL for a host that is not plain HTTP", func(t *testing.T) {
		r := require.New(t)
		capture := captureWarnings(t)
		resolver, err := url.New(url.WithBaseURL("registry.local:5000"), url.WithBaseURLScheme("http"))
		r.NoError(err)
		client := &recordingClient{}
		resolver.SetClient(client)

		r.ErrorContains(resolver.Ping(t.Context()), `host "registry.local:5000"`)
		r.Nil(client.url, "the ping must not reach the registry")
		r.Equal([]string{"registry.local:5000"}, capture.deprecated())
	})

	t.Run("accepts an http base URL for a plain HTTP host and deprecates the prefix", func(t *testing.T) {
		r := require.New(t)
		capture := captureWarnings(t)
		cfg := &httpv1alpha1.Config{Hosts: map[string]*httpv1alpha1.HostConfig{"registry.local:5000": hostCfg(new(true))}}
		resolver, err := url.New(url.WithBaseURL("registry.local:5000"), url.WithBaseURLScheme("http"), url.WithHTTPConfig(cfg))
		r.NoError(err)
		client := &recordingClient{}
		resolver.SetClient(client)

		r.NoError(resolver.Ping(t.Context()))
		r.NotNil(client.url, "the ping must reach the client")
		r.Equal("http", client.url.Scheme)
		r.Equal([]string{"registry.local:5000"}, capture.deprecated())
		r.Empty(capture.downgraded(), "an http:// prefix asked for cleartext, so nothing was downgraded")
	})

	t.Run("names the request host for docker.io", func(t *testing.T) {
		r := require.New(t)
		capture := captureWarnings(t)
		cfg := &httpv1alpha1.Config{ProtocolConfig: httpv1alpha1.ProtocolConfig{PlainHTTP: new(true)}}
		resolver, err := url.New(url.WithBaseURL("docker.io"), url.WithBaseURLScheme("https"), url.WithHTTPConfig(cfg))
		r.NoError(err)
		resolver.SetClient(&recordingClient{})

		r.NoError(resolver.Ping(t.Context()))
		r.Equal([]string{"registry-1.docker.io"}, capture.deprecated())
		r.Equal([]string{"registry-1.docker.io"}, capture.downgraded())
	})

	t.Run("honours per-host plain HTTP", func(t *testing.T) {
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

		cfg := &httpv1alpha1.Config{Hosts: map[string]*httpv1alpha1.HostConfig{serverHost: hostCfg(new(true))}}
		resolver, err := url.New(url.WithBaseURL(serverHost), url.WithHTTPConfig(cfg))
		r.NoError(err)
		r.NoError(resolver.Ping(t.Context()))

		resolver, err = url.New(url.WithBaseURL(serverHost))
		r.NoError(err)
		r.ErrorContains(resolver.Ping(t.Context()), "server gave HTTP response to HTTPS client", "without the host entry the ping must try HTTPS against a plain HTTP server")
	})

	t.Run("keys the host lookup on the request host", func(t *testing.T) {
		r := require.New(t)
		cfg := &httpv1alpha1.Config{
			ProtocolConfig: httpv1alpha1.ProtocolConfig{PlainHTTP: new(true)},
			Hosts:          map[string]*httpv1alpha1.HostConfig{"registry-1.docker.io": hostCfg(new(false))},
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

	t.Run("follows global plain HTTP over an https base URL", func(t *testing.T) {
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

		cfg := &httpv1alpha1.Config{ProtocolConfig: httpv1alpha1.ProtocolConfig{PlainHTTP: new(true)}}
		resolver, err := url.New(url.WithBaseURL(serverHost), url.WithBaseURLScheme("https"), url.WithHTTPConfig(cfg))
		r.NoError(err)
		r.NoError(resolver.Ping(t.Context()), "global plainHTTP wins over an https:// base URL")

		resolver, err = url.New(url.WithBaseURL(serverHost), url.WithBaseURLScheme("https"))
		r.NoError(err)
		r.ErrorContains(resolver.Ping(t.Context()), "server gave HTTP response to HTTPS client", "without the config the ping must use HTTPS")
	})
}
