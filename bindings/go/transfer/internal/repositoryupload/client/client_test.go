package client_test

import (
	"errors"
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

type pipeBlob struct{ *io.PipeReader }

func (b pipeBlob) ReadCloser() (io.ReadCloser, error) { return b.PipeReader, nil }

func TestClientPutBlob_RejectsEarlySuccess(t *testing.T) {
	r := require.New(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if err := http.NewResponseController(w).EnableFullDuplex(); err != nil {
			t.Error(err)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(srv.Close)
	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() })
	_, complete, err := client.New(nil, nil).PutBlob(t.Context(), srv.URL, pipeBlob{pr}, "", nil, nil)
	r.ErrorContains(err, "incomplete upload")
	r.False(complete)
}

type closeErrorBlob struct{}

func (closeErrorBlob) ReadCloser() (io.ReadCloser, error) {
	return &source{Reader: strings.NewReader("content"), closeErr: errors.New("checksum mismatch")}, nil
}

func TestClientPutBlob_PropagatesCloseVerificationError(t *testing.T) {
	r := require.New(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_, _ = io.Copy(io.Discard, req.Body)
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(srv.Close)
	_, complete, err := client.New(nil, nil).PutBlob(t.Context(), srv.URL, closeErrorBlob{}, "", nil, nil)
	r.ErrorContains(err, "checksum mismatch")
	r.False(complete)
}
