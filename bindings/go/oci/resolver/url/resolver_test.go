package url_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/registry/remote"

	"ocm.software/open-component-model/bindings/go/oci/resolver/url"
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
	assert.NoError(t, err)
	assert.NotNil(t, resolver)
}

func TestURLPathResolver_SetClient(t *testing.T) {
	resolver, err := url.New(url.WithBaseURL("http://example.com"))
	assert.NoError(t, err)
	repo, err := remote.NewRepository("example.com/test")
	assert.NoError(t, err)

	// Set the client
	resolver.SetClient(repo.Client)

	// Verify the client was set by using it
	store, err := resolver.StoreForReference(context.Background(), "example.com/test")
	assert.NoError(t, err)
	assert.NotNil(t, store)
}
func TestURLPathResolver_ComponentVersionReference(t *testing.T) {
	resolver, err := url.New(url.WithBaseURL("http://example.com"))
	assert.NoError(t, err)
	component := "test-component"
	version := "v1.0.0"
	expected := "http://example.com/component-descriptors/test-component:v1.0.0"
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
			component: "test-component",
			version:   "v1.0.0",
			expected:  "http://example.com/my-org/components/component-descriptors/test-component:v1.0.0",
		},
		{
			name:      "without subPath",
			baseURL:   "http://example.com",
			subPath:   "",
			component: "test-component",
			version:   "v1.0.0",
			expected:  "http://example.com/component-descriptors/test-component:v1.0.0",
		},
		{
			name:      "with nested subPath",
			baseURL:   "http://example.com",
			subPath:   "org/team/project",
			component: "test-component",
			version:   "v2.1.0",
			expected:  "http://example.com/org/team/project/component-descriptors/test-component:v2.1.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := []url.Option{url.WithBaseURL(tt.baseURL)}
			if tt.subPath != "" {
				opts = append(opts, url.WithSubPath(tt.subPath))
			}
			resolver, err := url.New(opts...)
			assert.NoError(t, err)
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
			assert.NoError(t, err)
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
			reference:   "invalid:reference",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver, err := url.New(url.WithBaseURL("http://example.com"))
			assert.NoError(t, err)
			store, err := resolver.StoreForReference(context.Background(), tt.reference)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, store)
				return
			}

			assert.NoError(t, err)
			assert.NotNil(t, store)
		})
	}
}

func TestURLPathResolver_Ping(t *testing.T) {
	ctx := context.Background()

	t.Run("ping with invalid URL fails", func(t *testing.T) {
		resolver, err := url.New(url.WithBaseURL("http://invalid.nonexistent.domain"))
		assert.NoError(t, err)

		err = resolver.Ping(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create registry client")
	})

	t.Run("ping with malformed URL fails", func(t *testing.T) {
		resolver, err := url.New(url.WithBaseURL("not-a-valid-url"))
		assert.NoError(t, err)

		err = resolver.Ping(ctx)
		assert.Error(t, err)
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
}
