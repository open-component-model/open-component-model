package client_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/client"
)

func TestClientSend_RejectsRedirectOfUpload(t *testing.T) {
	r := require.New(t)
	var loginHits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/login" {
			loginHits++
			_, _ = io.WriteString(w, "{}")
			return
		}
		_, _ = io.Copy(io.Discard, req.Body)
		http.Redirect(w, req, "/login", http.StatusFound)
	}))
	t.Cleanup(srv.Close)

	c := client.New(nil, nil)
	err := c.Send(t.Context(), http.MethodPut, srv.URL+"/upload", strings.NewReader("content"), -1, nil, nil)
	r.ErrorContains(err, "returned status 302")
	r.Zero(loginHits, "the redirect of an upload must not be followed")

	var out any
	r.NoError(c.Send(t.Context(), http.MethodGet, srv.URL+"/upload", nil, -1, nil, &out), "reads still follow redirects")
	r.Equal(1, loginHits)
}
