package repository

import (
	"context"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// StreamingResourceRepository is an optional capability of a ResourceRepository
// whose source content can be exposed as a lazy, replayable blob that streams on
// demand, so a by-value transfer to a remote target does not have to materialize
// the source to a temporary file first.
//
// Unlike DownloadResource, which returns content already materialized (and, for a
// file-backed blob, owned by the caller), DownloadResourceStream returns a blob
// whose ReadCloser re-issues the source request on each call. The blob therefore
// stays replayable from the beginning, honouring the blob.ReadOnlyBlob contract,
// without any temporary file.
//
// Integrity is preserved inline: when the source repository verifies downloaded
// content (for example against source-advertised checksums), the returned blob
// runs that verification over the bytes as they are read and reports a mismatch
// at end-of-stream (stream-then-verify). A caller that streams the blob straight
// to a target therefore still fails a corrupted transfer; it must treat the
// stream as untrusted until the read completes without error.
//
// Callers that need the content materialized, a size known up front, or a digest
// computed before reading must keep using DownloadResource. This capability only
// serves the streaming by-value transfer path.
type StreamingResourceRepository interface {
	ResourceRepository

	// DownloadResourceStream returns a lazy, replayable blob for the resource.
	// No content is fetched until the returned blob's ReadCloser is called, and
	// each ReadCloser call re-issues the source request from the beginning.
	DownloadResourceStream(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (blob.ReadOnlyBlob, error)
}

// IdempotentSource is an optional capability of a streaming source blob (see
// [StreamingResourceRepository]) that reports whether its underlying request can
// be safely issued more than once.
//
// The streaming by-value path may open a source blob more than once, for example
// to read the response headers (media type) before streaming the body. That is
// only safe when the request is idempotent, such as an HTTP GET or HEAD. A source
// backed by a non-idempotent request (for example a wget access with a non-GET
// verb and a body) MUST report false, and the caller must then avoid opening it
// more than once (typically by materializing it with DownloadResource instead).
//
// A blob that does not implement IdempotentSource must be treated as not safe to
// repeat.
type IdempotentSource interface {
	// Idempotent reports whether the source request can be repeated safely.
	Idempotent() bool
}
