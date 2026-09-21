package repository

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"ocm.software/open-component-model/bindings/go/blob"
	checksumhttpv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/checksum/http/v1alpha1/spec"
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/wget/checksum"
	"ocm.software/open-component-model/bindings/go/wget/checksum/httpverify"
	"ocm.software/open-component-model/bindings/go/wget/internal/download"
	accessspec "ocm.software/open-component-model/bindings/go/wget/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
	wgetcreds "ocm.software/open-component-model/bindings/go/wget/spec/credentials"
	identityv1 "ocm.software/open-component-model/bindings/go/wget/spec/identity/v1"
)

const (
	// hashAlgorithmSHA256 is the hash algorithm used for wget resource digests.
	hashAlgorithmSHA256 = "SHA-256"
	// genericBlobDigestV1 is the normalisation algorithm for a plain downloaded blob.
	genericBlobDigestV1 = "genericBlobDigest/v1"
)

var _ repository.ResourceRepository = (*ResourceRepository)(nil)

// ResourceRepository implements the ResourceRepository interface for wget access types.
type ResourceRepository struct {
	client           *http.Client
	maxDownloadSize  int64
	filesystemConfig *filesystemv1alpha1.Config
	// wgetConfig steers the digest processor's behavioural knobs — today, the
	// [checksumhttpv1alpha1.ChecksumPolicy] applied against the source when computing a
	// resource's digest. When nil, the digest is computed from the stream
	// without external verification (the pre-config default).
	wgetConfig *checksumhttpv1alpha1.Config
}

// NewResourceRepository creates a new wget resource repository. If filesystemConfig
// is non-nil, its TempFolder is used for the files downloaded bodies are streamed
// into; otherwise os.CreateTemp's default directory is used.
func NewResourceRepository(filesystemConfig *filesystemv1alpha1.Config, opts ...Option) *ResourceRepository {
	if filesystemConfig == nil {
		filesystemConfig = &filesystemv1alpha1.Config{}
	}
	options := &Options{}
	for _, opt := range opts {
		opt(options)
	}
	client := options.Client
	if client == nil {
		client = http.DefaultClient
	}
	var maxSize int64
	if options.MaxDownloadSize != nil {
		maxSize = *options.MaxDownloadSize
	} else {
		maxSize = DefaultMaxDownloadSize
	}
	return &ResourceRepository{
		client:           client,
		maxDownloadSize:  maxSize,
		filesystemConfig: filesystemConfig,
		wgetConfig:       options.WgetConfig,
	}
}

// GetResourceRepositoryScheme returns the scheme used by the wget resource repository.
func (r *ResourceRepository) GetResourceRepositoryScheme() *runtime.Scheme {
	return accessspec.Scheme
}

// GetResourceCredentialConsumerIdentity resolves the credential consumer identity for the given resource.
func (r *ResourceRepository) GetResourceCredentialConsumerIdentity(ctx context.Context, resource *descriptor.Resource) (runtime.Identity, error) {
	if resource == nil {
		return nil, fmt.Errorf("resource is required")
	}
	if resource.Access == nil {
		return nil, fmt.Errorf("resource access is required")
	}

	wget := v1.Wget{}
	if err := r.GetResourceRepositoryScheme().Convert(resource.Access, &wget); err != nil {
		return nil, fmt.Errorf("error converting resource access spec: %w", err)
	}

	if wget.URL == "" {
		return nil, fmt.Errorf("url is required")
	}

	identity, err := identityv1.IdentityFromURL(wget.URL)
	if err != nil {
		return nil, fmt.Errorf("error parsing wget URL to identity: %w", err)
	}

	return identity, nil
}

// DownloadResource downloads a resource from the URL specified in the wget access spec.
// The returned blob is backed by a file under the configured temp folder that outlives
// this call. The blob owns that file: callers should close it (it implements
// io.Closer) once they are done, and an unclosed blob has its file removed when it
// becomes unreachable.
func (r *ResourceRepository) DownloadResource(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (blob.ReadOnlyBlob, error) {
	b, _, err := r.download(ctx, resource, credentials)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// download streams the resource body into the configured temp folder and returns it
// as a file-backed blob owning that file. Extra options are appended after the
// repository's defaults, so callers (notably [ProcessResourceDigest]) can pass
// [download.WithDigestAlgorithms] to have SHA-256 computed inline during the stream.
// It also returns the resolved wget access spec, so callers avoid re-decoding it.
func (r *ResourceRepository) download(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed, extra ...download.Option) (*download.Blob, *v1.Wget, error) {
	if resource == nil {
		return nil, nil, fmt.Errorf("resource is required")
	}
	if resource.Access == nil {
		return nil, nil, fmt.Errorf("resource access is required")
	}

	wget := &v1.Wget{}
	if err := accessspec.Scheme.Convert(resource.Access, wget); err != nil {
		return nil, nil, fmt.Errorf("error converting resource access spec: %w", err)
	}

	var tempDir string
	if r.filesystemConfig.TempFolder != nil {
		tempDir = *r.filesystemConfig.TempFolder
	}

	opts := append([]download.Option{
		download.WithClient(r.client),
		download.WithMaxDownloadSize(r.maxDownloadSize),
		download.WithCredentials(credentials),
		download.WithTempDir(tempDir),
	}, extra...)

	b, err := download.Download(ctx, download.Request{
		URL:        wget.URL,
		MediaType:  wget.MediaType,
		Header:     wget.Header,
		Verb:       wget.Verb,
		Body:       wget.Body,
		NoRedirect: wget.NoRedirect,
	}, opts...)
	if err != nil {
		return nil, nil, err
	}
	return b, wget, nil
}

// UploadResource is not supported for wget access types.
func (r *ResourceRepository) UploadResource(ctx context.Context, res *descriptor.Resource, content blob.ReadOnlyBlob, credentials runtime.Typed) (*descriptor.Resource, error) {
	return nil, fmt.Errorf("upload is not supported for wget access type")
}

// GetResourceDigestProcessorCredentialConsumerIdentity resolves the credential consumer
// identity used when downloading the resource to compute its digest. It is the same identity
// used for a regular download, so credentials configured for the host apply to both.
func (r *ResourceRepository) GetResourceDigestProcessorCredentialConsumerIdentity(ctx context.Context, resource *descriptor.Resource) (runtime.Identity, error) {
	return r.GetResourceCredentialConsumerIdentity(ctx, resource)
}

// ProcessResourceDigest establishes the digest of a wget access resource. Two
// modes coexist, keyed on the checksum-http config:
//
//   - Fast path (config.DefaultChecksumPolicy.AccessDigest != nil, or the
//     host-scoped policy sets it): peek at the source-advertised checksum via
//     a HEAD + optional sidecar fetch. If any policy source yields a digest
//     whose algorithm is in AccessDigest.Algorithms, record it as the
//     resource's pinned digest without downloading the body. This is safe on
//     the access side — see [checksumhttpv1alpha1.AccessDigest] for the
//     rationale — and dramatically cheaper than streaming the whole artifact
//     just to hash it.
//   - Full-body path (default): download the referenced content and hash it
//     inline (single streaming pass, no re-read). If a policy applies, the
//     downloaded bytes are additionally verified against what the source
//     advertises. Any mismatch aborts before a digest is recorded.
//
// A pinned Digest on the resource is honoured in both modes: on the fast path
// it MUST agree with the source-advertised digest for the same algorithm; on
// the full-body path it MUST agree with the SHA-256 computed from the bytes.
// Pinned and policy are each checked against the same authority, never against
// each other.
func (r *ResourceRepository) ProcessResourceDigest(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (*descriptor.Resource, error) {
	policySpec := r.wgetConfig.PolicyForURL(policyURL(resource))
	policy, hasPolicy, err := toChecksumPolicy(policySpec)
	if err != nil {
		return nil, fmt.Errorf("invalid checksum policy for wget access digest: %w", err)
	}

	// Fast path — pin from source-advertised checksum without a body download.
	if hasPolicy && policySpec.AccessDigest != nil {
		result, done, err := r.processDigestViaPeek(ctx, resource, credentials, policy, policySpec.AccessDigest)
		if err != nil {
			return nil, err
		}
		if done {
			return result, nil
		}
		// Fall through to the full-body path when the source advertised nothing
		// and OnMissing is Compute (Fail was surfaced as an error above).
	}

	// Full-body path — download, hash, optionally verify.
	data, wget, err := r.download(ctx, resource, credentials,
		download.WithDigestAlgorithms(digestAlgorithms(policy)...),
	)
	if err != nil {
		return nil, fmt.Errorf("error downloading resource for digest processing: %w", err)
	}
	defer func() {
		if closeErr := data.Close(); closeErr != nil {
			slog.WarnContext(ctx, "failed to remove temporary file after digest processing", "err", closeErr)
		}
	}()

	if hasPolicy {
		if err := httpverify.Verify(ctx, r.client, credentials, wget.URL, policy, data); err != nil {
			return nil, fmt.Errorf("checksum verification failed for wget access %q: %w", wget.URL, err)
		}
	}

	sha := data.Digests()[checksum.StorageAlgorithm.OCMName]
	if sha == "" {
		return nil, fmt.Errorf("no computed %s digest available for wget access %q", checksum.StorageAlgorithm.OCMName, wget.URL)
	}

	resource = resource.DeepCopy()
	if resource.Digest == nil {
		resource.Digest = &descriptor.Digest{
			HashAlgorithm:          hashAlgorithmSHA256,
			NormalisationAlgorithm: genericBlobDigestV1,
			Value:                  sha,
		}
		return resource, nil
	}

	if resource.Digest.HashAlgorithm != hashAlgorithmSHA256 {
		return nil, fmt.Errorf("unsupported hash algorithm: expected %s, got %s", hashAlgorithmSHA256, resource.Digest.HashAlgorithm)
	}
	if resource.Digest.NormalisationAlgorithm != genericBlobDigestV1 {
		return nil, fmt.Errorf("unsupported normalisation algorithm: expected %s, got %s", genericBlobDigestV1, resource.Digest.NormalisationAlgorithm)
	}
	want := strings.TrimPrefix(resource.Digest.Value, "sha256:")
	if !strings.EqualFold(want, sha) {
		return nil, fmt.Errorf("digest mismatch: expected %s, got %s", resource.Digest.Value, sha)
	}

	return resource, nil
}

func (r *ResourceRepository) GetCredentialTypeScheme() *runtime.Scheme {
	return wgetcreds.Scheme
}

// processDigestViaPeek runs the fast, no-download path: it asks httpverify.Peek
// for a source-advertised digest constrained to the AccessDigest.Algorithms
// preference list. done==true means the digest has been established; done==false
// signals "no advertised digest, fall back to the full-body path" (allowed only
// when the policy's OnMissing is Compute).
func (r *ResourceRepository) processDigestViaPeek(
	ctx context.Context,
	resource *descriptor.Resource,
	credentials runtime.Typed,
	policy checksum.Policy,
	spec *checksumhttpv1alpha1.AccessDigest,
) (*descriptor.Resource, bool, error) {
	url := policyURL(resource)
	prefer, err := preferredAlgorithms(spec)
	if err != nil {
		return nil, false, fmt.Errorf("invalid checksum policy accessDigest.algorithms: %w", err)
	}

	exp, ok, err := httpverify.Peek(ctx, r.client, credentials, url, policy, prefer)
	if err != nil {
		return nil, false, fmt.Errorf("checksum peek failed for wget access %q: %w", url, err)
	}
	if !ok {
		if policy.OnMissing == checksum.Fail {
			return nil, false, fmt.Errorf("no advertised checksum for wget access %q and onMissing is %q", url, policy.OnMissing)
		}
		return nil, false, nil
	}

	out := resource.DeepCopy()
	if out.Digest != nil {
		// Pin cross-check: same algorithm and same value as what the source
		// advertises. Different algorithm on the pin is a hard error because
		// the operator has asked us not to hash the body ourselves.
		if !strings.EqualFold(out.Digest.HashAlgorithm, exp.Algorithm.OCMName) {
			return nil, false, fmt.Errorf("pinned digest algorithm %q does not match the source-advertised algorithm %q", out.Digest.HashAlgorithm, exp.Algorithm.OCMName)
		}
		if out.Digest.NormalisationAlgorithm != "" && out.Digest.NormalisationAlgorithm != genericBlobDigestV1 {
			return nil, false, fmt.Errorf("unsupported normalisation algorithm: expected %s, got %s", genericBlobDigestV1, out.Digest.NormalisationAlgorithm)
		}
		// The pinned Value may be bare hex or go-digest form ("sha256:<hex>",
		// "sha1:<hex>", …). Strip any trailing "alg:" prefix before comparison.
		pinnedHex := strings.ToLower(out.Digest.Value)
		if idx := strings.IndexByte(pinnedHex, ':'); idx >= 0 {
			pinnedHex = pinnedHex[idx+1:]
		}
		if !strings.EqualFold(pinnedHex, exp.Value) {
			return nil, false, fmt.Errorf("pinned digest %s does not match source-advertised %s digest %s", out.Digest.Value, exp.Algorithm.OCMName, exp.Value)
		}
	}
	out.Digest = &descriptor.Digest{
		HashAlgorithm:          exp.Algorithm.OCMName,
		NormalisationAlgorithm: genericBlobDigestV1,
		Value:                  exp.Value,
	}
	return out, true, nil
}

// preferredAlgorithms adapts the config-side AccessDigest algorithm list to a
// checksum.Algorithm list, defaulting to [sha256, sha512, sha1, md5] when the
// operator did not spell one out. Preference is order-sensitive: SHA-256 is
// preferred whenever the source advertises it, weaker algorithms fall through.
func preferredAlgorithms(spec *checksumhttpv1alpha1.AccessDigest) ([]checksum.Algorithm, error) {
	if spec == nil || len(spec.Algorithms) == 0 {
		return checksum.All, nil
	}
	return checksum.AlgorithmsFromExtensions(spec.Algorithms)
}

// policyURL extracts the URL used for wget-config host matching. A missing or
// non-wget access yields the empty string, which never matches a host key.
func policyURL(resource *descriptor.Resource) string {
	if resource == nil || resource.Access == nil {
		return ""
	}
	wget := v1.Wget{}
	if err := accessspec.Scheme.Convert(resource.Access, &wget); err != nil {
		return ""
	}
	return wget.URL
}

// toChecksumPolicy adapts an [checksumhttpv1alpha1.ChecksumPolicy] to the checksum package's
// Policy. Now that ChecksumSource.URL is a plain absolute URL (no templating),
// externalUrl sources work identically on the input and access paths.
func toChecksumPolicy(spec *checksumhttpv1alpha1.ChecksumPolicy) (checksum.Policy, bool, error) {
	if spec == nil {
		return checksum.Policy{}, false, nil
	}
	policy := checksum.Policy{OnMissing: checksum.Fail}
	if spec.OnMissing == checksumhttpv1alpha1.OnMissingCompute {
		policy.OnMissing = checksum.Compute
	}
	for i, src := range spec.Sources {
		algs, err := checksum.AlgorithmsFromExtensions(src.Algorithms)
		if err != nil {
			return checksum.Policy{}, false, fmt.Errorf("checksum policy source #%d: %w", i, err)
		}
		policy.Sources = append(policy.Sources, checksum.Source{
			Type:       checksum.SourceType(src.Type),
			Headers:    src.Headers,
			Algorithms: algs,
			URL:        src.URL,
		})
	}
	return policy, true, nil
}

// digestAlgorithms builds the download-package algorithm list from a resolved
// policy, always including SHA-256 (the storage algorithm).
func digestAlgorithms(policy checksum.Policy) []download.DigestAlgorithm {
	required := checksum.RequiredAlgorithms(policy)
	out := make([]download.DigestAlgorithm, 0, len(required))
	for _, alg := range required {
		out = append(out, download.DigestAlgorithm{
			Name: alg.OCMName,
			New:  alg.New,
		})
	}
	return out
}
