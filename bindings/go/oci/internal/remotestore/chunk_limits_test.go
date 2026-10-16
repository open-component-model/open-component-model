package remotestore

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/registry/remote"
)

func TestRegistryMaxChunkSize(t *testing.T) {
	tests := []struct {
		registry  string
		wantLimit int64
		wantKnown bool
	}{
		{registry: "ghcr.io", wantLimit: 4 << 20, wantKnown: true},
		{registry: "GHCR.io", wantLimit: 4 << 20, wantKnown: true},
		{registry: "ghcr.io:443", wantLimit: 4 << 20, wantKnown: true},
		{registry: "ghcr.io:5000", wantKnown: false},
		{registry: "notghcr.io", wantKnown: false},
		{registry: "europe-docker.pkg.dev", wantLimit: 0, wantKnown: true},
		{registry: "us-central1-docker.pkg.dev:443", wantLimit: 0, wantKnown: true},
		{registry: "europe-maven.pkg.dev", wantKnown: false},
		{registry: "docker.io", wantKnown: false},
		{registry: "localhost:5000", wantKnown: false},
	}
	for _, tc := range tests {
		t.Run(tc.registry, func(t *testing.T) {
			r := require.New(t)
			limit, known := registryMaxChunkSize(tc.registry)
			r.Equal(tc.wantKnown, known)
			r.Equal(tc.wantLimit, limit)
		})
	}
}

// storeForHost returns a RemoteStore whose repository is addressed by host, so
// registry-specific limits apply, while every connection is served by srv.
func storeForHost(t *testing.T, srv *httptest.Server, host string, chunkSize, threshold int64) *RemoteStore {
	t.Helper()
	repo, err := remote.NewRepository(host + "/test-repo")
	require.NoError(t, err)
	repo.PlainHTTP = true
	addr := srv.Listener.Addr().String()
	repo.Client = &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}}
	return &RemoteStore{Repository: repo, ChunkSize: chunkSize, ChunkThreshold: threshold}
}

// TestRemoteStore_KnownRegistryChunkLimit verifies that pushes to ghcr.io use
// chunks within its 4 MiB PATCH limit instead of the configured 16 MiB, so no
// chunk is rejected, and that a registry-advertised OCI-Chunk-Min-Length still
// takes precedence over the hardcoded limit.
func TestRemoteStore_KnownRegistryChunkLimit(t *testing.T) {
	const ghcrLimit = 4 << 20
	data := bytes.Repeat([]byte("0123456789abcdef"), (10<<20)/16)
	dig := digest.FromBytes(data)
	mediaType := "application/octet-stream"

	pushes := map[string]func(t *testing.T, store *RemoteStore) error{
		"Push": func(t *testing.T, store *RemoteStore) error {
			t.Helper()
			return store.Push(t.Context(), ociImageSpecV1.Descriptor{MediaType: mediaType, Digest: dig, Size: int64(len(data))}, bytes.NewReader(data))
		},
		"PushStreaming": func(t *testing.T, store *RemoteStore) error {
			t.Helper()
			_, err := store.PushStreaming(t.Context(), ociImageSpecV1.Descriptor{MediaType: mediaType}, bytes.NewReader(data))
			return err
		},
	}
	tests := []struct {
		name           string
		patchLimit     int
		chunkMinLength string
		wantPatchSizes []int
	}{
		{
			name:           "capped to ghcr.io limit",
			patchLimit:     ghcrLimit,
			wantPatchSizes: []int{ghcrLimit, ghcrLimit, 2 << 20},
		},
		{
			name:           "advertised minimum overrides limit",
			chunkMinLength: "6291456",
			wantPatchSizes: []int{6 << 20, 4 << 20},
		},
	}
	for _, tc := range tests {
		for pushName, push := range pushes {
			t.Run(tc.name+"/"+pushName, func(t *testing.T) {
				r := require.New(t)
				reg := newChunkedRegistry(t)
				reg.patchLimit = tc.patchLimit
				reg.chunkMinLength = tc.chunkMinLength
				srv := httptest.NewServer(reg.handler())
				t.Cleanup(srv.Close)

				r.NoError(push(t, storeForHost(t, srv, "ghcr.io", DefaultChunkSize, 1)))

				var patchSizes []int
				for _, req := range reg.requests {
					if req.method == http.MethodPatch {
						patchSizes = append(patchSizes, len(req.body))
					}
				}
				r.Equal(tc.wantPatchSizes, patchSizes)
				r.NotContains(reg.methods(), http.MethodDelete, "no chunk may be rejected")
				r.Equal(data, reg.uploaded)
			})
		}
	}
}

// TestRemoteStore_RegistryWithoutChunkedUpload verifies that pushes to Google
// Artifact Registry, which does not support chunked upload, never PATCH: Push
// stays monolithic and PushStreaming reports ErrStreamingUnavailable without a
// request so the caller buffers and pushes monolithically.
func TestRemoteStore_RegistryWithoutChunkedUpload(t *testing.T) {
	r := require.New(t)
	reg := newChunkedRegistry(t)
	srv := httptest.NewServer(reg.monolithicHandler())
	t.Cleanup(srv.Close)
	store := storeForHost(t, srv, "europe-docker.pkg.dev", DefaultChunkSize, 1)

	data := bytes.Repeat([]byte("x"), 20<<20)
	dig := digest.FromBytes(data)
	_, err := store.PushStreaming(t.Context(), ociImageSpecV1.Descriptor{MediaType: "application/octet-stream"}, bytes.NewReader(data))
	r.ErrorIs(err, ErrStreamingUnavailable)
	r.Empty(reg.requests)

	r.NoError(store.Push(t.Context(), ociImageSpecV1.Descriptor{MediaType: "application/octet-stream", Digest: dig, Size: int64(len(data))}, bytes.NewReader(data)))
	r.Equal([]string{http.MethodPost, http.MethodPut}, reg.methods())
	r.Equal(data, reg.monolithicBlobs[dig.String()])
}
