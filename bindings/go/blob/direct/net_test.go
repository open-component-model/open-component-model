package direct

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCompatibility_Net(t *testing.T) {
	data := "data"

	t.Run("http", func(t *testing.T) {
		// Create a temporary HTTP server that serves the data
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			wr, err := w.Write([]byte(data))
			assert.NoError(t, err)
			assert.Equal(t, len(data), wr)
		}))
		t.Cleanup(func() {
			server.Close()
		})

		resp, err := http.Get(server.URL)
		assert.NoError(t, err)

		b := NewFromHTTPResponse(resp)
		br, err := b.ReadCloser()
		assert.NoError(t, err)

		result, err := io.ReadAll(br)
		assert.NoError(t, err)
		assert.Equal(t, data, string(result))
		assert.Equal(t, int64(len(data)), b.Size())

		br2, err := b.ReadCloser()
		assert.NoError(t, err)
		result2, err := io.ReadAll(br2)
		assert.NoError(t, err)
		assert.Empty(t, result2, "http based reads are completely unbuffered and should not return the same data twice")
	})
}
