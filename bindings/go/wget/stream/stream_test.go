package stream_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	credv1 "ocm.software/open-component-model/bindings/go/wget/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/wget/stream"
)

func resource(access string) *descriptor.Resource {
	raw := &runtime.Raw{}
	if err := raw.UnmarshalJSON([]byte(access)); err != nil {
		panic(err)
	}
	return &descriptor.Resource{ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "r", Version: "1.0.0"}}, Access: raw}
}

func TestStreamer_OpenResource(t *testing.T) {
	const content = "chart bytes"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/missing":
			http.NotFound(w, r)
		case r.Header.Get("Authorization") != "Bearer token":
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		case r.Header.Get("Accept-Encoding") != "identity":
			http.Error(w, "compression must be disabled", http.StatusBadRequest)
		default:
			_, _ = io.WriteString(w, content)
		}
	}))
	t.Cleanup(srv.Close)
	creds := &credv1.WgetCredentials{Type: credv1.WgetCredentialsVersionedType, IdentityToken: "token"}

	tests := []struct {
		name        string
		access      string
		wantErr     string
		unsupported bool
	}{
		{name: "streams the response body with credentials", access: `{"type":"Wget/v1","url":"` + srv.URL + `/chart.tgz"}`},
		{name: "fails on a non-2xx status", access: `{"type":"Wget/v1","url":"` + srv.URL + `/missing"}`, wantErr: "returned status 404"},
		{name: "other access types are unsupported", access: `{"type":"OCIImage/v1","imageReference":"ghcr.io/a/b:1"}`, unsupported: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			rc, size, err := (&stream.Streamer{}).OpenResource(t.Context(), resource(tt.access), creds)
			switch {
			case tt.unsupported:
				r.True(errors.Is(err, errors.ErrUnsupported))
				return
			case tt.wantErr != "":
				r.ErrorContains(err, tt.wantErr)
				return
			}
			r.NoError(err)
			defer func() { _ = rc.Close() }()
			data, err := io.ReadAll(rc)
			r.NoError(err)
			r.Equal(content, string(data))
			r.Equal(int64(len(content)), size)
		})
	}
}
