package remotestore

import (
	"github.com/oras-project/oras-go/v3/content"
	"github.com/oras-project/oras-go/v3/registry/remote"

	"ocm.software/open-component-model/bindings/go/oci/spec"
)

// RemoteStore wraps *remote.Repository and overrides Push to optionally upload
// large blobs in chunks and adds PushStreaming.
type RemoteStore struct {
	*remote.Repository

	// ChunkSize is the target size in bytes for each PATCH chunk. If <= 0,
	// chunked upload is disabled and Push always delegates to the embedded
	// monolithic push. The registry-advertised OCI-Chunk-Min-Length (from the
	// session POST response) raises this floor for all but the final chunk.
	ChunkSize int64

	// ChunkThreshold is the minimum blob size in bytes for chunked upload to
	// engage. Blobs of Size < ChunkThreshold (and all manifests) use the
	// embedded monolithic Push. If <= 0, DefaultChunkThreshold is used. The
	// effective threshold is never below ChunkSize, so a blob that fits in a
	// single chunk is always pushed monolithically.
	ChunkThreshold int64
}

var (
	_ spec.Store       = (*RemoteStore)(nil) // general store spec
	_ content.Untagger = (*RemoteStore)(nil) // content.Untagger opt-in
)
