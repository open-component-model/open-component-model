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

			err = store.Untag(t.Context(), "latest")
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestRemoteStore_Untag_ReferenceValidation(t *testing.T) {
	tests := []struct {
		name      string
		reference string
		wantErr   bool
	}{
		// valid aliases — non-semver floating tags
		{"latest", "latest", false},
		{"stable", "stable", false},
		{"edge", "edge", false},
		{"my-component-alias", "my-component-alias", false},
		// digests
		{"digest sha256", "sha256:44136fa355ba77b9ad7b9e1dabe2dfe34ef0e91e2b5bff3e5e24b0bf09f28cbb", true},
		{"digest sha512", "sha512:abc123def456", true},
		// malformed
		{"empty string", "", true},
		{"colon separator", "not:a:tag", true},
		{"leading dot", ".badtag", true},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo, err := remote.NewRepository(srv.Listener.Addr().String() + "/test-repo")
			require.NoError(t, err)
			repo.PlainHTTP = true
			repo.Client = &http.Client{}

			store := &RemoteStore{Repository: repo}
			err = store.Untag(t.Context(), tc.reference)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
