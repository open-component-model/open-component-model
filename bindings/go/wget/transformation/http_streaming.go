package transformation

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	godigest "github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ocmhttp "ocm.software/open-component-model/bindings/go/http"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/wget/internal/download"
	wgetaccessv1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
	identityv1 "ocm.software/open-component-model/bindings/go/wget/spec/identity/v1"
	"ocm.software/open-component-model/bindings/go/wget/transformation/spec/v1alpha1"
)

const (
	// hashAlgorithmSHA256 is the hash algorithm recorded for streamed resource digests.
	hashAlgorithmSHA256 = "SHA-256"
	// genericBlobDigestV1 is the normalisation algorithm for a plain streamed blob.
	genericBlobDigestV1 = "genericBlobDigest/v1"
	// maxErrorBodyBytes bounds how much of a non-2xx response body is read into an
	// error message, so a hostile or verbose server cannot force unbounded reads.
	maxErrorBodyBytes = 4 << 10
)

// SourceRequest describes the source content an HTTPStreaming upload reads.
type SourceRequest struct {
	// Resource is the source resource with its original access.
	Resource *descriptor.Resource
	// Target is the resource as it will be published (its access resolved), so an opener can
	// check the content against what will be published.
	Target *descriptor.Resource
	// Credentials are the resolved source credentials; nil for a local resource.
	Credentials runtime.Typed
	// Local is set when Resource is a local resource of a component version
	// (HTTPStreamingSpec.ComponentVersion).
	Local *LocalSource
}

// LocalSource locates a local resource in its source component version.
type LocalSource struct {
	Repository repository.ComponentVersionRepository
	Component  string
	Version    string
}

// OpenedSource is the content to upload.
type OpenedSource struct {
	Blob blob.ReadOnlyBlob
	// Derived reports that Blob is a different representation than the source resource
	// digest describes (e.g. one layer of an OCI artifact), so the digest of the uploaded
	// bytes is recorded instead of verifying the source digest.
	Derived bool
}

// SourceOpener produces the content to upload for a source.
type SourceOpener func(ctx context.Context, src SourceRequest) (OpenedSource, error)

// HTTPStreamingTransformer streams a resource's content from its source access
// directly to an HTTP target (e.g. a PUT upload). The source blob is read through
// the injected ResourceRepository (dispatched by the source access type), or through
// a named SourceOpener when the spec selects one, and the body is piped straight into
// the request via an io.TeeReader, so the content is never buffered in memory or on
// disk by the transformer. The digest is computed during the stream (or verified
// against an existing one) and recorded on the target resource, whose published
// access is taken verbatim from the spec's TargetResource and may be of any type.
type HTTPStreamingTransformer struct {
	Scheme             *runtime.Scheme
	ResourceRepository repository.ResourceRepository
	CredentialProvider credentials.Resolver
	HTTPConfig         *httpv1alpha1.Config
	// RepoProvider resolves the source component version repository of local resources.
	RepoProvider repository.ComponentVersionRepositoryProvider
	// Openers maps HTTPStreamingSpec.Opener names to implementations.
	Openers map[string]SourceOpener
}

func (t *HTTPStreamingTransformer) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	var transformation v1alpha1.HTTPStreaming
	if err := t.Scheme.Convert(step, &transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to HTTPStreaming transformation: %w", err)
	}
	if transformation.Spec == nil {
		return nil, fmt.Errorf("spec is required for HTTPStreaming transformation")
	}
	if transformation.Spec.Resource == nil {
		return nil, fmt.Errorf("source resource is required")
	}
	if transformation.Spec.TargetResource == nil {
		return nil, fmt.Errorf("target resource is required")
	}
	if transformation.Spec.Request == nil {
		return nil, fmt.Errorf("upload request is required")
	}
	open, err := t.sourceOpener(transformation.Spec.Opener)
	if err != nil {
		return nil, err
	}
	if transformation.Output == nil {
		transformation.Output = &v1alpha1.HTTPStreamingOutput{}
	}

	srcResource := descriptor.ConvertFromV2Resource(transformation.Spec.Resource)
	targetResource := descriptor.ConvertFromV2Resource(transformation.Spec.TargetResource)

	// Request is the single source of truth for the outbound HTTP call. TargetResource is
	// published verbatim on success (only its digest is filled), so the published access
	// never carries the upload-only request fields.
	tw := *transformation.Spec.Request
	safeURL, err := redactedHTTPURL(tw.URL)
	if err != nil {
		return nil, err
	}

	opened, err := t.openSource(ctx, open, transformation.Spec, srcResource, targetResource)
	if err != nil {
		return nil, err
	}
	dstCreds, err := t.resolveTargetCredentials(ctx, tw.URL)
	if err != nil {
		return nil, err
	}
	srcBlob := opened.Blob
	rc, err := srcBlob.ReadCloser()
	if err != nil {
		return nil, fmt.Errorf("failed opening source resource stream: %w", err)
	}
	defer func() { _ = rc.Close() }()

	// TeeReader mirrors the streamed body into the hasher as it is uploaded, so the
	// digest is computed in a single pass without buffering the content.
	hasher := sha256.New()
	body := io.TeeReader(rc, hasher)

	method := tw.Verb
	if method == "" {
		method = http.MethodPut
	}
	req, err := http.NewRequestWithContext(ctx, method, tw.URL, body)
	if err != nil {
		return nil, fmt.Errorf("failed creating upload request: %w", err)
	}
	for k, vals := range tw.Header {
		for _, v := range vals {
			req.Header.Add(k, v)
		}
	}
	// Only derive the Content-Type from the media type when the user did not already set
	// one via the request headers above, so an explicit Content-Type is never overridden.
	if req.Header.Get("Content-Type") == "" {
		if contentType := targetContentType(tw, srcBlob); contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
	}
	if sizer, ok := srcBlob.(blob.SizeAware); ok {
		if size := sizer.Size(); size != blob.SizeUnknown {
			req.ContentLength = size
		}
	}

	slog.InfoContext(ctx, "streaming resource to HTTP target",
		"resource", srcResource.ToIdentity(),
		"targetURL", safeURL,
		"method", method)

	resp, err := t.send(ctx, req, tw.NoRedirect, dstCreds, safeURL, "upload")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	// Finalize the digest only after the whole body has been streamed to the target.
	computed := godigest.NewDigestFromBytes(godigest.SHA256, hasher.Sum(nil)).Encoded()
	slog.DebugContext(ctx, "streamed resource upload complete",
		"resource", srcResource.ToIdentity(),
		"digest", computed,
		"status", resp.StatusCode)

	replaceDigest := opened.Derived || transformation.Spec.Opener != "" && srcResource.Digest != nil && srcResource.Digest.NormalisationAlgorithm != genericBlobDigestV1
	targetResource.Digest, err = targetDigest(srcResource.Digest, computed, replaceDigest)
	if err != nil {
		return nil, err
	}
	if after := transformation.Spec.AfterUpload; after != nil {
		if err := t.sendAfterUpload(ctx, *after); err != nil {
			return nil, err
		}
	}

	// TargetResource already carries the published read access built at graph-build time,
	// so publish it verbatim (only the digest was filled above); no upload-only request
	// field can leak into the download access.
	v2Out, err := descriptor.ConvertToV2Resource(t.Scheme, targetResource)
	if err != nil {
		return nil, fmt.Errorf("failed converting target resource to v2 format: %w", err)
	}
	transformation.Output.Resource = v2Out
	return &transformation, nil
}

// sendAfterUpload issues the body-less follow-up request (POST unless a verb is set) with
// credentials resolved for its own URL.
func (t *HTTPStreamingTransformer) sendAfterUpload(ctx context.Context, tw wgetaccessv1.Wget) error {
	safeURL, err := redactedHTTPURL(tw.URL)
	if err != nil {
		return fmt.Errorf("after-upload request: %w", err)
	}
	creds, err := t.resolveTargetCredentials(ctx, tw.URL)
	if err != nil {
		return err
	}
	method := tw.Verb
	if method == "" {
		method = http.MethodPost
	}
	req, err := http.NewRequestWithContext(ctx, method, tw.URL, nil)
	if err != nil {
		return fmt.Errorf("failed creating after-upload request: %w", err)
	}
	for k, vals := range tw.Header {
		for _, v := range vals {
			req.Header.Add(k, v)
		}
	}
	slog.InfoContext(ctx, "sending after-upload request", "url", safeURL, "method", method)
	resp, err := t.send(ctx, req, tw.NoRedirect, creds, safeURL, "after-upload request")
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

// send applies credentials to req, executes it and rejects non-2xx responses. safeURL is
// the redacted URL used in errors; op names the request in errors. On success the caller
// owns the response body.
func (t *HTTPStreamingTransformer) send(ctx context.Context, req *http.Request, noRedirect bool, creds runtime.Typed, safeURL, op string) (*http.Response, error) {
	client := ocmhttp.New(ocmhttp.WithConfig(t.HTTPConfig))
	if noRedirect {
		client = download.CloneClientWithNoRedirect(client)
	}
	if err := download.ApplyCredentials(ctx, req, &client, creds); err != nil {
		return nil, fmt.Errorf("failed applying target credentials: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		// client.Do wraps errors in a *url.Error whose URL field carries the full
		// request URL including any query token. Redact it so the token never reaches
		// logs or the returned error.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			urlErr.URL = safeURL
		}
		return nil, fmt.Errorf("failed sending %s to %s: %w", op, safeURL, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer func() { _ = resp.Body.Close() }()
		// Include a bounded excerpt of the response body to aid debugging without
		// risking unbounded memory use on a hostile or verbose server.
		excerpt, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		if len(excerpt) > 0 {
			return nil, fmt.Errorf("%s to %s returned status %d: %s", op, safeURL, resp.StatusCode, strings.TrimSpace(string(excerpt)))
		}
		return nil, fmt.Errorf("%s to %s returned status %d", op, safeURL, resp.StatusCode)
	}
	return resp, nil
}

// redactedHTTPURL validates that raw is an http(s) URL and returns it without userinfo,
// query and fragment, so credentials and presigned params never leak into logs or errors.
func redactedHTTPURL(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid target url %q: %w", raw, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("target url must use the http or https scheme, got %q", parsed.Scheme)
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

// sourceOpener returns the opener registered under name, or the plain resource download
// when name is empty.
func (t *HTTPStreamingTransformer) sourceOpener(name string) (SourceOpener, error) {
	if name == "" {
		return t.openPlain, nil
	}
	opener, ok := t.Openers[name]
	if !ok {
		return nil, fmt.Errorf("unknown source opener %q", name)
	}
	return opener, nil
}

// openPlain streams the local resource from its component version, or else the downloaded
// source bytes, unchanged.
func (t *HTTPStreamingTransformer) openPlain(ctx context.Context, src SourceRequest) (OpenedSource, error) {
	if l := src.Local; l != nil {
		b, _, err := l.Repository.GetLocalResource(ctx, l.Component, l.Version, src.Resource.ToIdentity())
		return OpenedSource{Blob: b}, err
	}
	b, err := t.ResourceRepository.DownloadResource(ctx, src.Resource, src.Credentials)
	return OpenedSource{Blob: b}, err
}

// openSource prepares the SourceRequest (source component version of a local resource, or
// resolved source credentials) and runs open on it.
func (t *HTTPStreamingTransformer) openSource(ctx context.Context, open SourceOpener, spec *v1alpha1.HTTPStreamingSpec, srcResource, targetResource *descriptor.Resource) (OpenedSource, error) {
	req := SourceRequest{Resource: srcResource, Target: targetResource}
	var err error
	if cv := spec.ComponentVersion; cv != nil {
		if req.Local, err = t.localSource(ctx, cv); err != nil {
			return OpenedSource{}, err
		}
	} else if req.Credentials, err = t.resolveSourceCredentials(ctx, srcResource); err != nil {
		return OpenedSource{}, err
	}
	opened, err := open(ctx, req)
	if err != nil {
		return OpenedSource{}, fmt.Errorf("failed downloading source resource %v: %w", srcResource.ToIdentity(), err)
	}
	if opened.Blob == nil {
		return OpenedSource{}, fmt.Errorf("source opener returned no content for resource %v", srcResource.ToIdentity())
	}
	return opened, nil
}

// localSource resolves the repository of the source component version with its credentials.
func (t *HTTPStreamingTransformer) localSource(ctx context.Context, cv *v1alpha1.SourceComponentVersion) (*LocalSource, error) {
	if t.RepoProvider == nil {
		return nil, errors.New("streaming a local resource requires a component version repository provider")
	}
	if cv.Repository == nil || cv.Component == "" || cv.Version == "" {
		return nil, errors.New("componentVersion requires repository, component and version")
	}
	var creds runtime.Typed
	if t.CredentialProvider != nil {
		if consumerID, err := t.RepoProvider.GetComponentVersionRepositoryCredentialConsumerIdentity(ctx, cv.Repository); err == nil {
			if creds, err = t.CredentialProvider.Resolve(ctx, consumerID); err != nil && !errors.Is(err, credentials.ErrNotFound) {
				return nil, fmt.Errorf("failed resolving source repository credentials: %w", err)
			}
		}
	}
	repo, err := t.RepoProvider.GetComponentVersionRepository(ctx, cv.Repository, creds)
	if err != nil {
		return nil, fmt.Errorf("failed getting source component version repository: %w", err)
	}
	return &LocalSource{Repository: repo, Component: cv.Component, Version: cv.Version}, nil
}

// targetDigest verifies the source digest against the computed SHA-256 of the uploaded
// bytes and returns the digest to record on the target resource. With replace set, the
// uploaded bytes are a different representation than the source digest describes (e.g. the
// chart layer of an OCI artifact), so the digest of the uploaded bytes is recorded instead.
func targetDigest(src *descriptor.Digest, computed string, replace bool) (*descriptor.Digest, error) {
	if src == nil || replace {
		return &descriptor.Digest{
			HashAlgorithm:          hashAlgorithmSHA256,
			NormalisationAlgorithm: genericBlobDigestV1,
			Value:                  computed,
		}, nil
	}
	if src.HashAlgorithm != hashAlgorithmSHA256 {
		return nil, fmt.Errorf("unsupported hash algorithm: expected %s, got %s", hashAlgorithmSHA256, src.HashAlgorithm)
	}
	if src.NormalisationAlgorithm != genericBlobDigestV1 {
		return nil, fmt.Errorf("unsupported normalisation algorithm: expected %s, got %s", genericBlobDigestV1, src.NormalisationAlgorithm)
	}
	if src.Value != computed {
		return nil, fmt.Errorf("digest mismatch: expected %s, got %s", src.Value, computed)
	}
	return src.DeepCopy(), nil
}

// resolveSourceCredentials resolves credentials for the source resource by its consumer
// identity. A missing provider or ErrNotFound yields nil credentials; a failure to
// derive the consumer identity is a real error and is propagated.
func (t *HTTPStreamingTransformer) resolveSourceCredentials(ctx context.Context, resource *descriptor.Resource) (runtime.Typed, error) {
	if t.CredentialProvider == nil {
		return nil, nil
	}
	consumerID, err := t.ResourceRepository.GetResourceCredentialConsumerIdentity(ctx, resource)
	if err != nil {
		return nil, fmt.Errorf("failed deriving source consumer identity: %w", err)
	}
	if consumerID == nil {
		return nil, nil
	}
	creds, err := t.CredentialProvider.Resolve(ctx, consumerID)
	if err != nil {
		if errors.Is(err, credentials.ErrNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed resolving source credentials: %w", err)
	}
	return creds, nil
}

// resolveTargetCredentials resolves credentials for the target URL via its wget consumer
// identity. A missing provider or ErrNotFound yields nil credentials.
func (t *HTTPStreamingTransformer) resolveTargetCredentials(ctx context.Context, targetURL string) (runtime.Typed, error) {
	if t.CredentialProvider == nil {
		return nil, nil
	}
	identity, err := identityv1.IdentityFromURL(targetURL)
	if err != nil {
		return nil, fmt.Errorf("failed deriving target consumer identity: %w", err)
	}
	creds, err := t.CredentialProvider.Resolve(ctx, identity)
	if err != nil {
		if errors.Is(err, credentials.ErrNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed resolving target credentials: %w", err)
	}
	return creds, nil
}

// targetContentType prefers the target Wget access media type, falling back to the
// source blob's media type when it exposes one.
func targetContentType(tw wgetaccessv1.Wget, srcBlob blob.ReadOnlyBlob) string {
	if tw.MediaType != "" {
		return tw.MediaType
	}
	if mt, ok := srcBlob.(blob.MediaTypeAware); ok {
		if mediaType, known := mt.MediaType(); known {
			return mediaType
		}
	}
	return ""
}
