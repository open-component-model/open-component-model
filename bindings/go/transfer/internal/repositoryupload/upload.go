package repositoryupload

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/compression"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/chart"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/client"
	uploadv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/transformation/spec/v1alpha1"
	wgetaccess "ocm.software/open-component-model/bindings/go/wget/spec/access"
	wgetaccessv1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
)

const (
	// PollAttempts bounds how often the metadata a server records for uploaded content is polled
	// before the content is considered not to be recognized. The metadata can lag behind the
	// upload: Artifactory may calculate package properties asynchronously, Nexus indexes
	// components for search about 2 s after the upload (Nexus 3.96). 30 polls allow about 15 s for
	// a server under load.
	PollAttempts = 30
	// DefaultPollInterval is the wait between two metadata polls.
	DefaultPollInterval = 500 * time.Millisecond
)

// Backend is the vendor-specific part of an upload.
type Backend interface {
	// Name is "artifactory" or "nexus", used in logs and errors.
	Name() string
	// CredentialURLs returns the Helm repository URL and repository URL upload credentials are resolved for.
	CredentialURLs(spec *uploadv1alpha1.RepositoryUploadSpec) (helmRepo, repoURL string, err error)
	// Store reads the repository type from the server and returns the store for it, or the
	// unsupported-type error. The store sends its requests with c.
	Store(ctx context.Context, c *client.Client, spec *uploadv1alpha1.RepositoryUploadSpec, src *descriptor.Resource, interval time.Duration) (Store, error)
}

// Store uploads into one repository type. Its digests are empty when unknown.
type Store interface {
	// Chart reports that the store takes the packaged Helm chart located in the content (see chart.Locate),
	// uploaded as application/gzip, instead of the content as is.
	Chart() bool
	// URL is the upload location, for logs and errors (callers redact it).
	URL() string
	// Stored reports whether the repository already holds content with digest known ("" when unknown), or
	// made it available without an upload. It fails for a location holding content that must not be
	// overwritten. Stores whose server cannot look up the algorithm of known treat it as unknown.
	Stored(ctx context.Context, known digest.Digest) (bool, error)
	// Put uploads content and returns the digest of the bytes sent, see [client.DigestAlgorithm].
	Put(ctx context.Context, content blob.ReadOnlyBlob, mediaType string, known digest.Digest) (digest.Digest, error)
	// Discard removes uploaded content that must not be published. Stores that cannot remove it return
	// an error saying where it was left.
	Discard(ctx context.Context, uploaded digest.Digest) error
	// Publish returns the access of the stored content. Content the server did not recognize as the
	// repository type expects yields a *NotRecognizedError.
	Publish(ctx context.Context, stored digest.Digest, mediaType string) (runtime.Typed, error)
}

// NotRecognizedError reports content the server did not recognize as the package the repository expects.
type NotRecognizedError struct {
	// Kind is what the content should have been, e.g. "a helm chart" or "an npm package".
	Kind, Reason string
}

func (e *NotRecognizedError) Error() string { return e.Kind + ": " + e.Reason }

// Upload runs one repository upload and returns the transformation output. The upload location
// must be free, hold this resource's earlier upload, or already hold the content; content the
// server does not recognize, or whose digest does not match the source digest, is discarded again.
func (u *Uploader) Upload(ctx context.Context, spec *uploadv1alpha1.RepositoryUploadSpec, b Backend) (*uploadv1alpha1.RepositoryUploadOutput, error) {
	if err := validateSpec(spec); err != nil {
		return nil, fmt.Errorf("invalid %s upload: %w", b.Name(), err)
	}
	helmRepo, repoURL, err := b.CredentialURLs(spec)
	if err != nil {
		return nil, err
	}
	c, err := u.target(ctx, helmRepo, repoURL)
	if err != nil {
		return nil, err
	}
	src := descriptor.ConvertFromV2Resource(spec.Resource)
	st, err := b.Store(ctx, c, spec, src, u.interval())
	if err != nil {
		return nil, err
	}

	content, mediaType, err := u.source(ctx, spec, src)
	if err != nil {
		return nil, err
	}
	defer closeBlob(content)
	fromOCI := ociSource(src, mediaType)
	if st.Chart() {
		located, layoutChart, err := chart.Locate(ctx, content, mediaType, src.ToIdentity())
		if err != nil {
			return nil, err
		}
		defer closeBlob(located)
		content, fromOCI, mediaType = located, fromOCI || layoutChart, compression.MediaTypeGzip
	} else {
		mediaType = contentType(mediaType, spec.Resource)
	}

	expected, known, err := knownDigest(src.Digest, content, fromOCI)
	if err != nil {
		return nil, err
	}
	stored, err := st.Stored(ctx, known)
	if err != nil {
		return nil, err
	}
	logAttrs := func() []any {
		return []any{"server", b.Name(), "resource", src.ToIdentity(), "url", client.RedactURL(st.URL())}
	}
	dgst := known
	switch {
	case stored && st.Chart():
		slog.InfoContext(ctx, "reused helm chart content already stored in the helm repository", logAttrs()...)
	case stored:
		slog.InfoContext(ctx, "reused content already stored in the "+b.Name()+" repository", logAttrs()...)
	default:
		if dgst, err = st.Put(ctx, content, mediaType, known); err != nil {
			return nil, err
		}
		if known != "" && dgst != known {
			discard(ctx, st, dgst)
			return nil, fmt.Errorf("digest mismatch: expected %s, got %s", known, dgst)
		}
		if st.Chart() {
			slog.InfoContext(ctx, "uploaded helm chart", logAttrs()...)
		} else {
			slog.InfoContext(ctx, "uploaded resource content", logAttrs()...)
		}
	}

	access, err := st.Publish(ctx, dgst, mediaType)
	if nr := (*NotRecognizedError)(nil); errors.As(err, &nr) {
		discard(ctx, st, dgst)
		return nil, fmt.Errorf("content of resource %s is not %s: %s", src.ToIdentity(), nr.Kind, nr.Reason)
	}
	if err != nil {
		return nil, err
	}
	out := src.DeepCopy()
	out.Access = access
	out.Digest = uploadedDigest(src.Digest, expected, dgst)
	return u.output(out)
}

// discard removes uploaded content that must not be published, logging where it was left otherwise.
func discard(ctx context.Context, st Store, uploaded digest.Digest) {
	if err := st.Discard(ctx, uploaded); err != nil {
		slog.WarnContext(ctx, "failed removing uploaded content that must not be published", "url", client.RedactURL(st.URL()), "error", err)
	}
}

// closeBlob releases a blob that holds an open stream or file.
func closeBlob(b blob.ReadOnlyBlob) {
	if closer, ok := b.(io.Closer); ok {
		_ = closer.Close()
	}
}

// Poll calls try up to PollAttempts times, waiting interval between calls, until it reports done.
// It returns false without error when try never reported done, and ctx.Err() when ctx ends.
// Servers record the metadata of uploaded content asynchronously, so it is polled for.
func Poll(ctx context.Context, interval time.Duration, try func() (done bool, err error)) (bool, error) {
	for attempt := 1; ; attempt++ {
		if done, err := try(); done || err != nil {
			return done, err
		}
		if attempt == PollAttempts {
			return false, nil
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(interval):
		}
	}
}

// FileAccess returns the Wget/v1 access of the file stored at url with mediaType.
func FileAccess(url, mediaType string) runtime.Typed {
	return &wgetaccessv1.Wget{Type: wgetaccess.V1VersionedType, URL: url, MediaType: mediaType}
}
