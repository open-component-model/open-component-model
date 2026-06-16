package remotestore

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/errdef"
	"oras.land/oras-go/v2/registry/remote"
)

func TestRemoteStore_Untag(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantErr    error
	}{
		{"200 OK", http.StatusOK, nil},
		{"202 Accepted", http.StatusAccepted, nil},
		{"204 No Content", http.StatusNoContent, nil},
		{"404 Not Found", http.StatusNotFound, errdef.ErrNotFound},
		{"405 Method Not Allowed", http.StatusMethodNotAllowed, ErrTagDeletionDisabled},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.statusCode)
			}))
			t.Cleanup(srv.Close)

			repo, err := remote.NewRepository(srv.Listener.Addr().String() + "/test-repo")
			require.NoError(t, err)
			repo.PlainHTTP = true
			repo.Client = &http.Client{}

			store := &RemoteStore{Repository: repo}

			err = store.Untag(t.Context(), "v1.0.0")
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
