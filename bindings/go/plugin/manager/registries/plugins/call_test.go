package plugins

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
)

// TestStructs defines sample structs for testing
type (
	TestRequest struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}

	TestResponse struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}
)

func TestCall(t *testing.T) {
	// Test cases
	t.Run("successful call without payload or result", func(t *testing.T) {
		// Setup test server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/test-endpoint", r.URL.Path)
			assert.Equal(t, http.MethodGet, r.Method)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		// Call the function
		err := Call(context.Background(), server.Client(), types.TCP, server.URL, "test-endpoint", http.MethodGet)
		assert.NoError(t, err)
	})

	t.Run("successful call with payload and result", func(t *testing.T) {
		// Setup test server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify request
			assert.Equal(t, "/test-endpoint", r.URL.Path)
			assert.Equal(t, http.MethodPost, r.Method)

			// Read and verify payload
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			var req TestRequest
			err = json.Unmarshal(body, &req)
			require.NoError(t, err)
			assert.Equal(t, "test", req.Name)
			assert.Equal(t, 42, req.Value)

			// Send response
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			resp := TestResponse{
				Status:  "success",
				Message: "Operation completed",
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		// Create payload and result
		payload := TestRequest{
			Name:  "test",
			Value: 42,
		}
		var result TestResponse

		// Call the function
		err := Call(
			context.Background(),
			server.Client(),
			types.TCP,
			server.URL,
			"test-endpoint",
			http.MethodPost,
			WithPayload(payload),
			WithResult(&result),
		)
		assert.NoError(t, err)
		assert.Equal(t, "success", result.Status)
		assert.Equal(t, "Operation completed", result.Message)
	})

	t.Run("call with headers", func(t *testing.T) {
		// Setup test server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify headers
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
			assert.Equal(t, "Bearer token123", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		// Create headers
		headers := []KV{
			{Key: "Content-Type", Value: "application/json"},
			{Key: "Authorization", Value: "Bearer token123"},
		}

		// Call the function
		err := Call(
			context.Background(),
			server.Client(),
			types.TCP,
			server.URL,
			"test-endpoint",
			http.MethodGet,
			WithHeaders(headers),
		)
		assert.NoError(t, err)
	})

	t.Run("call with single header", func(t *testing.T) {
		// Setup test server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify header
			assert.Equal(t, "Bearer token456", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		// Call the function
		err := Call(
			context.Background(),
			server.Client(),
			types.TCP,
			server.URL,
			"test-endpoint",
			http.MethodGet,
			WithHeader(KV{Key: "Authorization", Value: "Bearer token456"}),
		)
		assert.NoError(t, err)
	})

	t.Run("call with query parameters", func(t *testing.T) {
		// Setup test server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify query parameters
			query := r.URL.Query()
			assert.Equal(t, "value1", query.Get("param1"))
			assert.Equal(t, "value2", query.Get("param2"))
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		// Create query parameters
		queryParams := []KV{
			{Key: "param1", Value: "value1"},
			{Key: "param2", Value: "value2"},
		}

		// Call the function
		err := Call(
			context.Background(),
			server.Client(),
			types.TCP,
			server.URL,
			"test-endpoint",
			http.MethodGet,
			WithQueryParams(queryParams),
		)
		assert.NoError(t, err)
	})

	t.Run("handle non-200 response", func(t *testing.T) {
		// Setup test server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, `Invalid request`, http.StatusBadRequest)
		}))
		defer server.Close()

		// Call the function
		err := Call(
			context.Background(),
			server.Client(),
			types.TCP,
			server.URL,
			"test-endpoint",
			http.MethodGet,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "plugin returned status code 400")
		assert.Contains(t, err.Error(), "Invalid request")
	})

	t.Run("handle non-200 response with empty body", func(t *testing.T) {
		// Setup test server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		// Call the function
		err := Call(
			context.Background(),
			server.Client(),
			types.TCP,
			server.URL,
			"test-endpoint",
			http.MethodGet,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "plugin returned status code: 500 (no details were given)")
	})

	t.Run("handle invalid JSON response", func(t *testing.T) {
		// Setup test server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("This is not valid JSON"))
		}))
		defer server.Close()

		// Call the function
		var result TestResponse
		err := Call(
			context.Background(),
			server.Client(),
			types.TCP,
			server.URL,
			"test-endpoint",
			http.MethodGet,
			WithResult(&result),
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode response from plugin")
	})

	t.Run("handle connection errors", func(t *testing.T) {
		// Create a client with a non-existent URL
		err := Call(
			context.Background(),
			http.DefaultClient,
			types.TCP,
			"http://localhost:12345", // This should be an unused port
			"test-endpoint",
			http.MethodGet,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to send request to plugin")
	})

	t.Run("handle invalid payload", func(t *testing.T) {
		// Create an invalid payload (a channel cannot be marshaled to JSON)
		invalidPayload := make(chan int)

		// Call the function
		err := Call(
			context.Background(),
			http.DefaultClient,
			types.TCP,
			"http://example.com",
			"test-endpoint",
			http.MethodPost,
			WithPayload(invalidPayload),
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to marshal payload")
	})

	t.Run("handle context cancellation", func(t *testing.T) {
		// Create a cancelled context
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		// Setup test server that delays
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// This part doesn't get executed due to cancelled context
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		// Call the function with cancelled context
		err := Call(
			ctx,
			server.Client(),
			types.TCP,
			server.URL,
			"test-endpoint",
			http.MethodGet,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "context canceled")
	})

	t.Run("unix socket connection type base URL", func(t *testing.T) {
		// We can't actually test a Unix socket connection easily in a unit test
		// But we can validate that the URL is constructed correctly
		mockClient := &http.Client{
			Transport: &mockTransport{
				roundTripFn: func(req *http.Request) (*http.Response, error) {
					// Verify the URL starts with http://unix/
					assert.True(t, strings.HasPrefix(req.URL.String(), "http://unix/"))
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader("")),
					}, nil
				},
			},
		}

		err := Call(
			context.Background(),
			mockClient,
			types.Socket,
			"/tmp/socket.sock", // This path is ignored for the test
			"test-endpoint",
			http.MethodGet,
		)
		assert.NoError(t, err)
	})

	t.Run("multiple options used together", func(t *testing.T) {
		// Setup test server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify request method and path
			assert.Equal(t, "/test-endpoint", r.URL.Path)
			assert.Equal(t, http.MethodPost, r.Method)

			// Verify headers
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
			assert.Equal(t, "Bearer token789", r.Header.Get("Authorization"))

			// Verify query parameters
			query := r.URL.Query()
			assert.Equal(t, "value1", query.Get("param1"))
			assert.Equal(t, "value2", query.Get("param2"))

			// Read and verify payload
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			var req TestRequest
			err = json.Unmarshal(body, &req)
			require.NoError(t, err)
			assert.Equal(t, "test", req.Name)
			assert.Equal(t, 42, req.Value)

			// Send response
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			resp := TestResponse{
				Status:  "success",
				Message: "Operation completed",
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		// Create payload and result
		payload := TestRequest{
			Name:  "test",
			Value: 42,
		}
		var result TestResponse

		// Create headers
		headers := []KV{
			{Key: "Content-Type", Value: "application/json"},
		}

		// Create query parameters
		queryParams := []KV{
			{Key: "param1", Value: "value1"},
			{Key: "param2", Value: "value2"},
		}

		// Call the function with multiple options
		err := Call(
			context.Background(),
			server.Client(),
			types.TCP,
			server.URL,
			"test-endpoint",
			http.MethodPost,
			WithPayload(payload),
			WithResult(&result),
			WithHeaders(headers),
			WithHeader(KV{Key: "Authorization", Value: "Bearer token789"}),
			WithQueryParams(queryParams),
		)
		assert.NoError(t, err)
		assert.Equal(t, "success", result.Status)
		assert.Equal(t, "Operation completed", result.Message)
	})
}

// mockTransport is a mock implementation of http.RoundTripper for testing
type mockTransport struct {
	roundTripFn func(req *http.Request) (*http.Response, error)
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.roundTripFn(req)
}

// TestCallOptions tests the individual option functions
func TestCallOptions(t *testing.T) {
	t.Run("WithPayload", func(t *testing.T) {
		options := &CallOptions{}
		payload := TestRequest{Name: "test", Value: 42}

		fn := WithPayload(payload)
		fn(options)

		assert.Equal(t, payload, options.Payload)
	})

	t.Run("WithResult", func(t *testing.T) {
		options := &CallOptions{}
		result := &TestResponse{}

		fn := WithResult(result)
		fn(options)

		assert.Equal(t, result, options.Result)
	})

	t.Run("WithHeaders", func(t *testing.T) {
		options := &CallOptions{}
		headers := []KV{
			{Key: "Content-Type", Value: "application/json"},
			{Key: "Authorization", Value: "Bearer token"},
		}

		fn := WithHeaders(headers)
		fn(options)

		assert.Equal(t, headers, options.Headers)
	})

	t.Run("WithHeader", func(t *testing.T) {
		options := &CallOptions{}
		header := KV{Key: "Authorization", Value: "Bearer token"}

		fn := WithHeader(header)
		fn(options)

		assert.Equal(t, []KV{header}, options.Headers)

		// Test appending another header
		header2 := KV{Key: "Content-Type", Value: "application/json"}
		fn2 := WithHeader(header2)
		fn2(options)

		assert.Equal(t, []KV{header, header2}, options.Headers)
	})

	t.Run("WithQueryParams", func(t *testing.T) {
		options := &CallOptions{}
		queryParams := []KV{
			{Key: "param1", Value: "value1"},
			{Key: "param2", Value: "value2"},
		}

		fn := WithQueryParams(queryParams)
		fn(options)

		assert.Equal(t, queryParams, options.QueryParams)
	})
}
