// Package integrity binds downloaded content to the digest a component descriptor
// records for it.
//
// Repositories hand the content they download to [Verify] together with the digest of
// the resource. The returned blob fails, at the latest when it is read to its end, if
// the content does not match:
//
//   - genericBlobDigest/v1 (or no normalisation) is a hash over the raw bytes. A digest
//     computed while the content was written ([VerifyPrecomputed]) or a blob that already
//     verifies against the same digest is reused, so content is never hashed twice.
//   - ociArtifactDigest/v1 names the root manifest or index of an OCI artifact. Callers
//     that resolve the artifact themselves check its root with [VerifyOCIArtifact];
//     content delivered as an archive is introspected by an [ArtifactVerifier] while it
//     is read.
//
// Content without a digest passes unverified with a warning, content excluded from
// signatures passes silently. A digest that is present but unusable fails.
package integrity

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/opencontainers/go-digest"
	slogcontext "github.com/veqryn/slog-context"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/verified"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// ErrUnverifiable reports a digest that the content cannot be checked against.
var ErrUnverifiable = errors.New("content cannot be verified against its digest")

// ArtifactVerifier introspects an OCI artifact delivered as an archive (OCI layout, artifact set).
type ArtifactVerifier interface {
	// Accepts reports whether content of mediaType is an archive this verifier can introspect.
	Accepts(mediaType string) bool
	// Verify returns rc wrapped so that reading it to its end (or closing it) verifies that the
	// archive's main artifact has digest root and is complete. size is the byte size of the
	// archive, or [blob.SizeUnknown]; when known, the read reaching it reports the result.
	// ctx bounds the returned reader and carries the logger for what verification reports.
	Verify(ctx context.Context, rc io.ReadCloser, root digest.Digest, size int64) io.ReadCloser
}

type options struct {
	artifacts ArtifactVerifier
	logAttrs  []slog.Attr
}

func newOptions(opts []Option) options {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// Option configures verification.
type Option func(*options)

// WithArtifactVerifier verifies ociArtifactDigest/v1 digests of archives v accepts.
// Without it, such content is [ErrUnverifiable].
func WithArtifactVerifier(v ArtifactVerifier) Option {
	return func(o *options) {
		o.artifacts = v
	}
}

// WithLogAttrs adds attrs (such as the identity of the resource) to what is logged
// when content is used without verification.
func WithLogAttrs(attrs ...slog.Attr) Option {
	return func(o *options) {
		o.logAttrs = append(o.logAttrs, attrs...)
	}
}

type kind int

const (
	// kindNone is content without a digest to verify against.
	kindNone kind = iota
	// kindBlob is content whose raw bytes hash to the digest.
	kindBlob
	// kindArtifact is an OCI artifact whose root manifest or index has the digest.
	kindArtifact
)

// Verify returns content bound to expected: the returned blob reports expected as its
// digest and fails, at the latest when it is read to its end or closed, if the content
// does not match.
// For an OCI artifact verified while it is read, ctx bounds those reads: once it is
// done, reading the returned blob fails.
//
// It takes ownership of content: on error, content is closed if it is an [io.Closer].
func Verify(ctx context.Context, expected *descriptor.Digest, content blob.ReadOnlyBlob, opts ...Option) (blob.ReadOnlyBlob, error) {
	k, want, err := classify(expected)
	if err != nil {
		return nil, release(content, err)
	}

	switch k {
	case kindNone:
		logUnverified(ctx, expected, newOptions(opts).logAttrs)
		return content, nil
	case kindArtifact:
		verifying, err := newArtifactBlob(ctx, content, want, opts)
		if err != nil {
			return nil, release(content, err)
		}
		return verifying, nil
	}

	if v, ok := content.(*verified.Blob); ok {
		if got, known := v.Digest(); known {
			switch got := digest.Digest(got); {
			case got == want:
				return content, nil
			case got.Algorithm() == want.Algorithm():
				return nil, release(content, mismatch(want, got))
			}
		}
	}

	return &digestBlob{base: content, digest: want, verifyOnRead: true}, nil
}

// VerifyPrecomputed is [Verify] for content whose producer hashed it while writing it:
// if computed is of the algorithm of expected, it is compared right away and the content
// is not hashed again. computed may be empty if no digest was computed.
func VerifyPrecomputed(ctx context.Context, expected *descriptor.Digest, computed digest.Digest, content blob.ReadOnlyBlob, opts ...Option) (blob.ReadOnlyBlob, error) {
	k, want, err := classify(expected)
	if err == nil && k == kindBlob && computed != "" && computed.Algorithm() == want.Algorithm() {
		if computed != want {
			return nil, release(content, mismatch(want, computed))
		}
		return &digestBlob{base: content, digest: want}, nil
	}
	return Verify(ctx, expected, content, opts...)
}

// VerifiedOnRead reports whether content, as returned by [Verify] or [VerifyPrecomputed],
// is checked against its digest only while it is read: its bytes are hashed, or an
// archive is introspected, and a mismatch fails the read. Otherwise the digest was
// checked when content was returned, against what content is addressed by (such as the
// OCI descriptor of a stored blob, or a digest computed while it was written).
//
// A caller that needs the outcome without using the content reads it to its end only
// if VerifiedOnRead reports true.
func VerifiedOnRead(content blob.ReadOnlyBlob) bool {
	switch content := content.(type) {
	case *digestBlob:
		return content.verifyOnRead
	case *artifactBlob:
		return true
	default:
		return false
	}
}

// VerifyContent reads content to its end and reports whether it matches expected.
// It neither takes ownership of content nor closes it.
func VerifyContent(ctx context.Context, expected *descriptor.Digest, content blob.ReadOnlyBlob, opts ...Option) error {
	k, want, err := classify(expected)
	if err != nil {
		return err
	}

	var verifying blob.ReadOnlyBlob
	switch k {
	case kindNone:
		logUnverified(ctx, expected, newOptions(opts).logAttrs)
		return nil
	case kindArtifact:
		if verifying, err = newArtifactBlob(ctx, content, want, opts); err != nil {
			return err
		}
	default:
		verifying = &digestBlob{base: content, digest: want, verifyOnRead: true}
	}

	rc, err := verifying.ReadCloser()
	if err != nil {
		return err
	}
	_, err = blob.CopyReader(io.Discard, rc)
	return errors.Join(err, rc.Close())
}

// VerifyOCIArtifact checks that root, the digest of a resolved OCI artifact's manifest
// or index, is the digest expected records. It is meant for callers that resolved the
// artifact and verify its graph themselves.
//
// genericBlobDigest/v1 is accepted as well, because it historically labelled the root
// digests of OCI artifacts.
func VerifyOCIArtifact(ctx context.Context, expected *descriptor.Digest, root digest.Digest, opts ...Option) error {
	want, err := ParseDigest(expected)
	if err != nil {
		return err
	}
	if want == "" {
		logUnverified(ctx, expected, newOptions(opts).logAttrs)
		return nil
	}

	switch n := expected.NormalisationAlgorithm; n {
	case "", descriptor.GenericBlobDigestV1, descriptor.OCIArtifactDigestV1:
		if want != root {
			return fmt.Errorf("OCI artifact digest mismatch: expected %s, got %s", want, root)
		}
		return nil
	default:
		return unsupportedNormalisation(n)
	}
}

// PrecomputeAlgorithm returns the algorithm to hash content with while it is downloaded,
// so that [VerifyPrecomputed] does not need to hash it again. It reports false if
// expected is not a digest over the raw bytes of the content.
func PrecomputeAlgorithm(expected *descriptor.Digest) (digest.Algorithm, bool) {
	k, want, err := classify(expected)
	if err != nil || k != kindBlob {
		return "", false
	}
	return want.Algorithm(), true
}

func classify(expected *descriptor.Digest) (kind, digest.Digest, error) {
	want, err := ParseDigest(expected)
	if err != nil {
		return kindNone, "", err
	}
	if want == "" {
		return kindNone, "", nil
	}

	switch n := expected.NormalisationAlgorithm; n {
	case "", descriptor.GenericBlobDigestV1:
		return kindBlob, want, nil
	case descriptor.OCIArtifactDigestV1:
		return kindArtifact, want, nil
	default:
		return kindNone, "", unsupportedNormalisation(n)
	}
}

func newArtifactBlob(ctx context.Context, content blob.ReadOnlyBlob, root digest.Digest, opts []Option) (*artifactBlob, error) {
	o := newOptions(opts)

	var mediaType string
	if aware, ok := content.(blob.MediaTypeAware); ok {
		mediaType, _ = aware.MediaType()
	}
	if o.artifacts == nil || !o.artifacts.Accepts(mediaType) {
		return nil, fmt.Errorf("%w: %s requires an OCI layout or artifact set, but the content has media type %q",
			ErrUnverifiable, descriptor.OCIArtifactDigestV1, mediaType)
	}

	// The blob is read after Verify returns, so it keeps ctx: reading stays bound to it,
	// and what the verifier logs names the content like the other warnings of this package.
	if len(o.logAttrs) > 0 {
		args := make([]any, len(o.logAttrs))
		for i, attr := range o.logAttrs {
			args[i] = attr
		}
		ctx = slogcontext.With(ctx, args...)
	}
	return &artifactBlob{ctx: ctx, base: content, root: root, verifier: o.artifacts}, nil
}

func logUnverified(ctx context.Context, expected *descriptor.Digest, attrs []slog.Attr) {
	if expected != nil && (strings.EqualFold(expected.HashAlgorithm, descriptor.NoDigest) ||
		strings.EqualFold(expected.NormalisationAlgorithm, descriptor.ExcludeFromSignature)) {
		slog.LogAttrs(ctx, slog.LevelDebug, "content is excluded from signature, it is used without verification", attrs...)
		return
	}
	slog.LogAttrs(ctx, slog.LevelWarn, "no digest recorded for the content, it is used without verification", attrs...)
}

func unsupportedNormalisation(normalisation string) error {
	return fmt.Errorf("%w: unsupported normalisation algorithm %q", ErrUnverifiable, normalisation)
}

func mismatch(want, got digest.Digest) error {
	return fmt.Errorf("digest mismatch: expected %s, got %s", want, got)
}

// release closes refused content, so that what it holds (such as a temporary file)
// does not leak.
func release(content blob.ReadOnlyBlob, err error) error {
	closer, ok := content.(io.Closer)
	if !ok {
		return err
	}
	if closeErr := closer.Close(); closeErr != nil {
		return errors.Join(err, fmt.Errorf("failed to release unverifiable content: %w", closeErr))
	}
	return err
}
