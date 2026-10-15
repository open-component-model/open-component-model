package repository

import (
	"context"
	"io"
	"sync"

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
// whose ReadCloser re-issues the source request on each call.
//
// Integrity is verified inline (stream-then-verify): a mismatch against the
// resource digest or a source-advertised checksum fails the final Read and the
// Close of the reader. A caller that streams the blob to a target must treat
// the content as untrusted until the read completes without error.
//
// Callers that need a size or digest before reading must use DownloadResource.
// Use [StreamOrDownloadResource] to pick the right method for a single
// by-value transfer.
type StreamingResourceRepository interface {
	ResourceRepository

	// DownloadResourceStream returns a lazy, replayable blob for the resource.
	// No content is fetched until the returned blob's ReadCloser is called, and
	// each ReadCloser call re-issues the source request from the beginning.
	DownloadResourceStream(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (blob.ReadOnlyBlob, error)
}

// IdempotentSource is an optional capability of a streaming source blob (see
// [StreamingResourceRepository]) that reports whether its underlying request can
// be safely issued more than once, such as an HTTP GET. A source backed by a
// non-idempotent request (for example a wget access with a POST verb and a body)
// must report false.
//
// A blob that does not implement IdempotentSource must be treated as not safe to
// repeat.
type IdempotentSource interface {
	// Idempotent reports whether the source request can be repeated safely.
	Idempotent() bool
}

// StreamOrDownloadResource returns the content of res for a single by-value
// transfer and reports whether that content is a lazy stream (true) or was
// downloaded with DownloadResource (false).
//
// A lazy stream is only used when repo implements [StreamingResourceRepository]
// and the stream reports itself as an [IdempotentSource] that is idempotent: a
// consumer can open a blob more than once (for example a push that falls back
// to a buffered upload), which must not repeat a non-idempotent request. Such a
// source is downloaded with DownloadResource instead, which sends the request
// once. A stream that does not implement IdempotentSource is returned as is and
// reported as not streamed, because its semantics are unknown (for example a
// registry that falls back to DownloadResource for a plugin that cannot
// stream).
//
// When the media type of a lazy stream is not known up front, the stream is
// opened here so that the media type is read from the response. The first
// ReadCloser call on the returned blob hands out that open reader, so no
// additional request is sent. Later ReadCloser calls open the source again.
//
// The caller must close the returned blob if it implements [io.Closer]. This
// releases a temporary file of a downloaded blob, or a reader that was opened
// but never consumed.
func StreamOrDownloadResource(ctx context.Context, repo ResourceRepository, res *descriptor.Resource, credentials runtime.Typed) (blob.ReadOnlyBlob, bool, error) {
	streamingRepo, ok := repo.(StreamingResourceRepository)
	if !ok {
		b, err := repo.DownloadResource(ctx, res, credentials)
		return b, false, err
	}
	b, err := streamingRepo.DownloadResourceStream(ctx, res, credentials)
	if err != nil {
		return nil, false, err
	}
	idem, ok := b.(IdempotentSource)
	if !ok {
		return b, false, nil
	}
	if !idem.Idempotent() {
		// No request was sent yet: DownloadResourceStream defers all I/O to the
		// first ReadCloser call.
		closeBlob(b)
		b, err := repo.DownloadResource(ctx, res, credentials)
		return b, false, err
	}
	if mt, ok := b.(blob.MediaTypeAware); ok {
		if _, known := mt.MediaType(); known {
			return b, true, nil
		}
	}
	rc, err := b.ReadCloser()
	if err != nil {
		closeBlob(b)
		return nil, false, err
	}
	return &preopenedBlob{base: b, pending: rc}, true, nil
}

func closeBlob(b blob.ReadOnlyBlob) {
	if c, ok := b.(io.Closer); ok {
		_ = c.Close()
	}
}

// preopenedBlob hands out a reader that was already opened on its base with the
// first ReadCloser call, and delegates later calls to the base.
type preopenedBlob struct {
	base blob.ReadOnlyBlob

	mu      sync.Mutex
	pending io.ReadCloser
}

var (
	_ blob.ReadOnlyBlob   = (*preopenedBlob)(nil)
	_ blob.MediaTypeAware = (*preopenedBlob)(nil)
	_ IdempotentSource    = (*preopenedBlob)(nil)
	_ io.Closer           = (*preopenedBlob)(nil)
)

func (b *preopenedBlob) ReadCloser() (io.ReadCloser, error) {
	b.mu.Lock()
	rc := b.pending
	b.pending = nil
	b.mu.Unlock()
	if rc != nil {
		return rc, nil
	}
	return b.base.ReadCloser()
}

func (b *preopenedBlob) MediaType() (string, bool) {
	if mt, ok := b.base.(blob.MediaTypeAware); ok {
		return mt.MediaType()
	}
	return "", false
}

func (b *preopenedBlob) Idempotent() bool {
	if idem, ok := b.base.(IdempotentSource); ok {
		return idem.Idempotent()
	}
	return false
}

// Close releases a reader that was never handed out and then the base. The
// error of the unconsumed reader is ignored: it reports the incomplete read,
// which is expected here.
func (b *preopenedBlob) Close() error {
	b.mu.Lock()
	rc := b.pending
	b.pending = nil
	b.mu.Unlock()
	if rc != nil {
		_ = rc.Close()
	}
	if c, ok := b.base.(io.Closer); ok {
		return c.Close()
	}
	return nil
}
