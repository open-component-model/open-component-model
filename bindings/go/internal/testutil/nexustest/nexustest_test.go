package nexustest_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/internal/testutil/nexustest"
)

func TestRequest(t *testing.T) {
	type seen struct {
		Method      string `json:"method"`
		User        string `json:"user"`
		Password    string `json:"password"`
		ContentType string `json:"contentType"`
		Body        string `json:"body"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		user, password, _ := req.BasicAuth()
		body, _ := io.ReadAll(req.Body)
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(seen{Method: req.Method, User: user, Password: password, ContentType: req.Header.Get("Content-Type"), Body: string(body)})
	}))
	t.Cleanup(server.Close)

	tests := []struct {
		name string
		body []byte
		want seen
	}{
		{name: "without body", want: seen{Method: http.MethodGet, User: "admin", Password: "pw"}},
		{name: "with json body", body: []byte(`{"a":1}`), want: seen{Method: http.MethodGet, User: "admin", Password: "pw", ContentType: "application/json", Body: `{"a":1}`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)

			status, body := nexustest.Request(t, http.MethodGet, server.URL, "pw", tt.body)

			r.Equal(http.StatusAccepted, status)
			var got seen
			r.NoError(json.Unmarshal(body, &got))
			r.Equal(tt.want, got)
		})
	}
}
