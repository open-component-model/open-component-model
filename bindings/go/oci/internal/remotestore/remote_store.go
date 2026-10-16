package remotestore

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"path"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/errdef"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"

	"ocm.software/open-component-model/bindings/go/oci/spec"
)

// ErrTagDeletionDisabled is returned when the registry responds with 405 Method Not Allowed
// to a tag-deletion request, indicating that the registry does not support tag deletion
// (e.g. REGISTRY_STORAGE_DELETE_ENABLED is not set).
var ErrTagDeletionDisabled = fmt.Errorf("registry does not support tag deletion (405 Method Not Allowed)")

// RemoteStore wraps *remote.Repository and adds content.Untagger support plus
// chunked blob upload (see Push and PushStreaming).
//
// oras-go implements content.Untagger only for its local OCI layout store
// (content/oci.Store). The remote registry client (registry/remote.Repository)
// intentionally omits it: the OCI Distribution Spec treats
// DELETE /v2/<name>/manifests/<tag> as optional, and not all registries honor it.
//
// oras-go's remote.Repository pushes blobs in one request only. RemoteStore
// uploads blobs of at least ChunkThreshold in chunks (see Push), and blobs
// whose digest or size is unknown up front in chunks while computing them (see
// PushStreaming).
type RemoteStore struct {
	*remote.Repository

	// ChunkSize is the target size in bytes for each PATCH chunk. If <= 0,
	// DefaultChunkSize is used; it is capped at MaxChunkSize. The
	// registry-advertised OCI-Chunk-Min-Length (from the session POST response)
	// raises this floor for all but the final chunk.
	ChunkSize int64

	// ChunkThreshold is the minimum size in bytes of a blob with a known digest
	// and size for Push to upload it in chunks. Smaller blobs, blobs that fit in
	// a single chunk and all manifests are pushed in one request. If <= 0,
	// DefaultChunkThreshold is used. Blobs of unknown size or digest are always
	// chunked (see PushStreaming).
	ChunkThreshold int64
}

var (
	_ spec.Store       = (*RemoteStore)(nil) // general store spec
	_ content.Untagger = (*RemoteStore)(nil) // content.Untagger opt-in
)

// Untag removes the given tag from the remote registry without deleting the underlying manifest.
// The registry must have tag deletion enabled; a 405 response means it is disabled server-side.
func (r *RemoteStore) Untag(ctx context.Context, reference string) error {
	ref := r.Reference
	ref.Reference = reference
	if err := ref.ValidateReferenceAsTag(); err != nil {
		return fmt.Errorf("invalid tag reference %q: %w", reference, err)
	}
	ctx = auth.AppendRepositoryScope(ctx, ref, auth.ActionDelete)

	endpoint := r.endpoint(path.Join("/v2", ref.Repository, "manifests", reference))
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint.String(), http.NoBody)
	if err != nil {
		return fmt.Errorf("failed to build delete request for alias %q: %w", reference, err)
	}

	resp, err := r.do(req)
	if err != nil {
		return fmt.Errorf("failed to delete alias %q: %w", reference, err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.Error("Failed to close response body for alias", "reference", reference, "err", err)
		}
	}()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusAccepted, http.StatusNoContent:
		return nil
	case http.StatusNotFound:
		return errdef.ErrNotFound
	case http.StatusMethodNotAllowed:
		return ErrTagDeletionDisabled
	default:
		return parseErrorResponse(resp)
	}
}

// Referrers lists referrers via [Referrers], falling back to the tag schema on NAME_UNKNOWN.
func (r *RemoteStore) Referrers(ctx context.Context, desc ociImageSpecV1.Descriptor, artifactType string, onReferrers func(referrers []ociImageSpecV1.Descriptor) error) error {
	return Referrers(ctx, r.Repository, desc, artifactType, onReferrers)
}

// Predecessors lists predecessors via [Predecessors], falling back to the tag schema on NAME_UNKNOWN.
func (r *RemoteStore) Predecessors(ctx context.Context, desc ociImageSpecV1.Descriptor) ([]ociImageSpecV1.Descriptor, error) {
	return Predecessors(ctx, r.Repository, desc)
}
