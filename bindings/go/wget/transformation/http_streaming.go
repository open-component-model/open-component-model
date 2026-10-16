package transformation

import (
	"context"
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
	"ocm.software/open-component-model/bindings/go/wget/httpauth"
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

// HTTPStreamingTransformer streams a resource's content from its source access
// directly to an HTTP target (e.g. a PUT upload). The source blob is read through
// the injected ResourceRepository (dispatched by the source access type) and the
// body is hashed as the request reads it, so the content is
// never buffered in memory or on disk by the transformer. The digest is computed
// during the stream (or verified against an existing one) and recorded on the
// target resource, which carries a Wget access at the resolved target URL.
type HTTPStreamingTransformer struct {
	Scheme             *runtime.Scheme
	ResourceRepository repository.ResourceRepository
	CredentialProvider credentials.Resolver
	HTTPConfig         *httpv1alpha1.Config
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
	if transformation.Output == nil {
		transformation.Output = &v1alpha1.HTTPStreamingOutput{}
	}

	srcResource := descriptor.ConvertFromV2Resource(transformation.Spec.Resource)
	targetResource := descriptor.ConvertFromV2Resource(transformation.Spec.TargetResource)

	// Request is the single source of truth for the outbound HTTP call. TargetResource is
	// published verbatim on success (only its digest is filled), so the published access
	// never carries the upload-only request fields.
	tw := *transformation.Spec.Request
	parsedTarget, err := url.Parse(tw.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid target url %q: %w", tw.URL, err)
	}
	if parsedTarget.Scheme != "http" && parsedTarget.Scheme != "https" {
		return nil, fmt.Errorf("target url must use the http or https scheme, got %q", parsedTarget.Scheme)
	}
	// safeURL strips userinfo and query so credentials/presigned params never leak into logs or errors.
	safeURL := *parsedTarget
	safeURL.User = nil
	safeURL.RawQuery = ""
	safeURL.Fragment = ""

	srcCreds, err := t.resolveSourceCredentials(ctx, srcResource)
	if err != nil {
		return nil, err
	}
	dstCreds, err := t.resolveTargetCredentials(ctx, tw.URL)
	if err != nil {
		return nil, err
	}

	// The wget and s3 repositories return a lazy blob, so the common wget/s3 ->
	// remote path needs no temporary file. A non-idempotent wget request is
	// downloaded once into a temporary file instead. The HTTP target may retain
	// bytes if verification fails at EOF. No delete operation is defined for this
	// target.
	srcBlob, err := t.ResourceRepository.DownloadResource(ctx, srcResource, srcCreds)
	if err != nil {
		return nil, fmt.Errorf("failed downloading source resource %v: %w", srcResource.ToIdentity(), err)
	}
	// Releases an open response of a streamed source, or the temporary file of a
	// downloaded one.
	defer func() {
		if c, ok := srcBlob.(io.Closer); ok {
			_ = c.Close()
		}
	}()
	rc, err := srcBlob.ReadCloser()
	if err != nil {
		return nil, fmt.Errorf("failed opening source resource stream: %w", err)
	}
	size := blob.SizeUnknown
	if sizer, ok := srcBlob.(blob.SizeAware); ok {
		size = sizer.Size()
	}
	body := newUploadBody(rc, size, godigest.SHA256)
	defer func() { _ = body.Close() }()

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
	if size != blob.SizeUnknown {
		req.ContentLength = size
	}

	client := ocmhttp.New(ocmhttp.WithConfig(t.HTTPConfig))
	if tw.NoRedirect {
		client = download.CloneClientWithNoRedirect(client)
	}
	if err := httpauth.Apply(ctx, req, &client, dstCreds); err != nil {
		return nil, fmt.Errorf("failed applying target credentials: %w", err)
	}

	slog.InfoContext(ctx, "streaming resource to HTTP target",
		"resource", srcResource.ToIdentity(),
		"targetURL", safeURL.String(),
		"method", method)

	resp, err := client.Do(req)
	closeErr := body.Close()
	if err != nil {
		// client.Do wraps errors in a *url.Error whose URL field carries the full
		// request URL including any query token. Redact it so the token never reaches
		// logs or the returned error.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			urlErr.URL = safeURL.String()
		}
		return nil, fmt.Errorf("failed uploading to %s: %w", safeURL.String(), err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Include a bounded excerpt of the response body to aid debugging without
		// risking unbounded memory use on a hostile or verbose server.
		excerpt, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		if len(excerpt) > 0 {
			return nil, fmt.Errorf("upload to %s returned status %d: %s", safeURL.String(), resp.StatusCode, strings.TrimSpace(string(excerpt)))
		}
		return nil, fmt.Errorf("upload to %s returned status %d", safeURL.String(), resp.StatusCode)
	}

	if closeErr != nil {
		return nil, fmt.Errorf("failed verifying source resource %v: %w", srcResource.ToIdentity(), closeErr)
	}
	computed := body.Digest().Encoded()
	slog.DebugContext(ctx, "streamed resource upload complete",
		"resource", srcResource.ToIdentity(),
		"digest", computed,
		"status", resp.StatusCode)

	if targetResource.Digest, err = uploadedDigest(srcResource.Digest, computed); err != nil {
		return nil, err
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

// uploadedDigest returns the digest of the uploaded resource: the source digest
// after it was checked against the computed SHA-256 value, or the computed value
// when the source has no digest.
func uploadedDigest(src *descriptor.Digest, computed string) (*descriptor.Digest, error) {
	if src == nil {
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
