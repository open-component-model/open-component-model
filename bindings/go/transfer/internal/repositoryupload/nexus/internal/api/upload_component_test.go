package api_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/client"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/nexus/internal/api"
)

type closeErrorBlob struct{}

func (closeErrorBlob) ReadCloser() (io.ReadCloser, error) {
	return closeErrorReader{Reader: strings.NewReader("content")}, nil
}

type closeErrorReader struct{ io.Reader }

func (closeErrorReader) Close() error { return errors.New("checksum mismatch") }

func TestUploadComponent_PropagatesSourceCloseError(t *testing.T) {
	r := require.New(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_, _ = io.Copy(io.Discard, req.Body)
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(srv.Close)
	repo, err := api.New(client.New(nil, nil), srv.URL, "raw")
	r.NoError(err)
	_, err = repo.UploadComponent(t.Context(), nil, "raw.asset1", "content", closeErrorBlob{}, "", "text/plain")
	r.ErrorContains(err, "checksum mismatch")
}
