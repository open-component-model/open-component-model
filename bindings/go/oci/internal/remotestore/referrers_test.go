package remotestore

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/registry/remote"
)

// TestRemoteStore_Predecessors_NameUnknownReferrersAPI covers registries such as xpkg.crossplane.io that
// answer the referrers API with 404 NAME_UNKNOWN for an existing repository. Referrers are optional, so
// discovery must fall back to the referrers tag schema instead of failing the copy of the subject.
func TestRemoteStore_Predecessors_NameUnknownReferrersAPI(t *testing.T) {
	subject := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Digest:    digest.FromString("subject"),
		Size:      7,
	}
	referrer := ociImageSpecV1.Descriptor{
		MediaType:    ociImageSpecV1.MediaTypeImageManifest,
		Digest:       digest.FromString("signature"),
		Size:         9,
		ArtifactType: "application/vnd.example.signature",
	}
	tagSchemaIndex, err := json.Marshal(ociImageSpecV1.Index{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ociImageSpecV1.MediaTypeImageIndex,
		Manifests: []ociImageSpecV1.Descriptor{referrer},
	})
	require.NoError(t, err)
	referrersTag := "sha256-" + subject.Digest.Encoded()

	tests := []struct {
		name          string
		tagSchemaHit  bool
		referrersCode int
		referrersBody string
		want          []ociImageSpecV1.Descriptor
		wantErr       bool
	}{
		{
			name:          "NAME_UNKNOWN falls back to tag schema",
			tagSchemaHit:  true,
			referrersCode: http.StatusNotFound,
			referrersBody: `{"errors":[{"code":"NAME_UNKNOWN","message":"repository name not known to registry"}]}`,
			want:          []ociImageSpecV1.Descriptor{referrer},
		},
		{
			name:          "NAME_UNKNOWN without referrers tag yields no referrers",
			referrersCode: http.StatusNotFound,
			referrersBody: `{"errors":[{"code":"NAME_UNKNOWN","message":"repository name not known to registry"}]}`,
			want:          nil,
		},
		{
			name:          "other registry errors still fail",
			referrersCode: http.StatusInternalServerError,
			referrersBody: `{"errors":[{"code":"UNKNOWN","message":"boom"}]}`,
			wantErr:       true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			var referrersAPICalls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				switch {
				case strings.HasPrefix(req.URL.Path, "/v2/test-repo/referrers/"):
					referrersAPICalls.Add(1)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(tc.referrersCode)
					_, _ = w.Write([]byte(tc.referrersBody))
				case req.URL.Path == "/v2/test-repo/manifests/"+referrersTag && tc.tagSchemaHit:
					w.Header().Set("Content-Type", ociImageSpecV1.MediaTypeImageIndex)
					w.Header().Set("Docker-Content-Digest", digest.FromBytes(tagSchemaIndex).String())
					_, _ = w.Write(tagSchemaIndex)
				default:
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusNotFound)
					_, _ = w.Write([]byte(`{"errors":[{"code":"MANIFEST_UNKNOWN","message":"manifest unknown"}]}`))
				}
			}))
			t.Cleanup(srv.Close)

			repo, err := remote.NewRepository(srv.Listener.Addr().String() + "/test-repo")
			r.NoError(err)
			repo.PlainHTTP = true
			repo.Client = &http.Client{}
			store := &RemoteStore{Repository: repo}

			got, err := store.Predecessors(t.Context(), subject)
			if tc.wantErr {
				r.Error(err)
				return
			}
			r.NoError(err)
			r.Equal(tc.want, got)

			// The fallback must not pin the shared repository to the tag schema: NAME_UNKNOWN is also
			// returned for repositories that do not exist yet and gain referrers API support later.
			_, err = store.Predecessors(t.Context(), subject)
			r.NoError(err)
			r.EqualValues(2, referrersAPICalls.Load())
		})
	}
}
