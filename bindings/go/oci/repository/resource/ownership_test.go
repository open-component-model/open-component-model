package resource

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/oci/spec/annotations"
)

func TestLookupOwners(t *testing.T) {
	// The first two cases fail before any registry round-trip, so they need no client.
	cases := []struct {
		name    string
		ref     string
		wantErr string
	}{
		{
			name:    "rejects an unparseable reference",
			ref:     "ftp://ghcr.io/acme/image:v1.0.0",
			wantErr: "parsing image reference",
		},
		{
			name:    "rejects a reference without a tag or digest",
			ref:     "ghcr.io/acme/image",
			wantErr: "must include a tag or digest",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LookupOwners(t.Context(), tc.ref, nil)
			require.ErrorContains(t, err, tc.wantErr)
		})
	}

	t.Run("returns parsed referrers and skips ones without ownership annotations", func(t *testing.T) {
		const (
			repoName     = "acme/image"
			tag          = "v1.0.0"
			componentA   = "ocm.software/a"
			versionA     = "1.0.0"
			componentB   = "ocm.software/b"
			versionB     = "2.0.0"
			subjectBytes = `{"subject":"manifest"}`
		)
		subjectDigest := digest.FromString(subjectBytes)

		artifactJSON := func(name string) string {
			b, _ := json.Marshal(annotations.ArtifactOCIAnnotation{
				Identity: map[string]string{"name": name},
				Kind:     annotations.ArtifactKindResource,
			})
			return string(b)
		}

		index := ociImageSpecV1.Index{
			MediaType: ociImageSpecV1.MediaTypeImageIndex,
			Manifests: []ociImageSpecV1.Descriptor{
				{
					MediaType:    ociImageSpecV1.MediaTypeImageManifest,
					Digest:       digest.FromString("ref-a"),
					Size:         42,
					ArtifactType: annotations.OwnershipArtifactType,
					Annotations: map[string]string{
						annotations.OwnershipComponentName:    componentA,
						annotations.OwnershipComponentVersion: versionA,
						annotations.ArtifactAnnotationKey:     artifactJSON("resource-a"),
					},
				},
				{
					// Same artifactType but missing ownership annotations —
					// must be skipped (ErrNotAnOwnershipReferrer branch).
					MediaType:    ociImageSpecV1.MediaTypeImageManifest,
					Digest:       digest.FromString("ref-no-annotations"),
					Size:         42,
					ArtifactType: annotations.OwnershipArtifactType,
				},
				{
					MediaType:    ociImageSpecV1.MediaTypeImageManifest,
					Digest:       digest.FromString("ref-b"),
					Size:         42,
					ArtifactType: annotations.OwnershipArtifactType,
					Annotations: map[string]string{
						annotations.OwnershipComponentName:    componentB,
						annotations.OwnershipComponentVersion: versionB,
						annotations.ArtifactAnnotationKey:     artifactJSON("resource-b"),
					},
				},
			},
		}
		indexBytes, err := json.Marshal(index)
		require.NoError(t, err)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/v2/":
				w.WriteHeader(http.StatusOK)
			case r.Method == http.MethodHead && r.URL.Path == "/v2/"+repoName+"/manifests/"+tag:
				w.Header().Set("Content-Type", ociImageSpecV1.MediaTypeImageManifest)
				w.Header().Set("Content-Length", strconv.Itoa(len(subjectBytes)))
				w.Header().Set("Docker-Content-Digest", subjectDigest.String())
				w.WriteHeader(http.StatusOK)
			case r.Method == http.MethodGet && r.URL.Path == "/v2/"+repoName+"/referrers/"+subjectDigest.String():
				w.Header().Set("Content-Type", ociImageSpecV1.MediaTypeImageIndex)
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(indexBytes)
			default:
				t.Logf("unexpected request: %s %s", r.Method, r.URL.String())
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		t.Cleanup(server.Close)

		serverURL, err := url.Parse(server.URL)
		require.NoError(t, err)
		imageRef := "http://" + serverURL.Host + "/" + repoName + ":" + tag

		owners, err := LookupOwners(t.Context(), imageRef, nil)
		require.NoError(t, err)
		require.Len(t, owners, 2, "two ownership referrers expected; one had no annotations and must be skipped")

		// Order matches the index order — the function preserves the
		// Referrers API ordering.
		assert.Equal(t, componentA, owners[0].ComponentName)
		assert.Equal(t, versionA, owners[0].ComponentVersion)
		assert.Equal(t, annotations.ArtifactKindResource, owners[0].Artifact.Kind)
		assert.Equal(t, "resource-a", owners[0].Artifact.Identity["name"])

		assert.Equal(t, componentB, owners[1].ComponentName)
		assert.Equal(t, versionB, owners[1].ComponentVersion)
		assert.Equal(t, "resource-b", owners[1].Artifact.Identity["name"])
	})

	t.Run("propagates malformed artifact annotation as a parse error", func(t *testing.T) {
		const (
			repoName     = "acme/image"
			tag          = "v1.0.0"
			subjectBytes = `{"subject":"manifest"}`
		)
		subjectDigest := digest.FromString(subjectBytes)

		index := ociImageSpecV1.Index{
			MediaType: ociImageSpecV1.MediaTypeImageIndex,
			Manifests: []ociImageSpecV1.Descriptor{
				{
					MediaType:    ociImageSpecV1.MediaTypeImageManifest,
					Digest:       digest.FromString("ref-bad"),
					Size:         42,
					ArtifactType: annotations.OwnershipArtifactType,
					Annotations: map[string]string{
						annotations.OwnershipComponentName:    "ocm.software/x",
						annotations.OwnershipComponentVersion: "1.0.0",
						annotations.ArtifactAnnotationKey:     "{not valid json",
					},
				},
			},
		}
		indexBytes, err := json.Marshal(index)
		require.NoError(t, err)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodHead && strings.HasSuffix(r.URL.Path, "/manifests/"+tag):
				w.Header().Set("Content-Type", ociImageSpecV1.MediaTypeImageManifest)
				w.Header().Set("Content-Length", strconv.Itoa(len(subjectBytes)))
				w.Header().Set("Docker-Content-Digest", subjectDigest.String())
				w.WriteHeader(http.StatusOK)
			case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/referrers/"+subjectDigest.String()):
				w.Header().Set("Content-Type", ociImageSpecV1.MediaTypeImageIndex)
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(indexBytes)
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		t.Cleanup(server.Close)

		serverURL, err := url.Parse(server.URL)
		require.NoError(t, err)
		imageRef := "http://" + serverURL.Host + "/" + repoName + ":" + tag

		_, err = LookupOwners(t.Context(), imageRef, nil)
		require.ErrorContains(t, err, "parsing ownership referrer")
	})
}
