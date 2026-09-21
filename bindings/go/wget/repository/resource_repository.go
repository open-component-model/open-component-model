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

// ProcessResourceDigest computes the digest of a wget resource by downloading the
// referenced content and hashing it inline (a single streaming pass — no re-read).
// When [ResourceRepository.wgetConfig] resolves a [checksumhttpv1alpha1.ChecksumPolicy] for
// the resource's URL, the downloaded bytes are also verified against it: header
// (RFC 9530 / x-checksum-*) and sibling-URL sources are honoured exactly as on
// the input-method side, and any mismatch aborts before a digest is recorded.
//
// When the resource already carries a Digest, the computed value is verified
// against it independently of the policy: both are checked against the actual
// bytes, never against each other.
func (r *ResourceRepository) ProcessResourceDigest(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (*descriptor.Resource, error) {
	// Ask the download package to compute every hash the policy may need in the
	// same streaming pass that writes the body to disk — no godigest re-read.
	policySpec := r.wgetConfig.PolicyForURL(policyURL(resource))
	policy, hasPolicy, err := toChecksumPolicy(policySpec)
	if err != nil {
		return nil, fmt.Errorf("invalid checksum policy for wget access digest: %w", err)
	}
	data, wget, err := r.download(ctx, resource, credentials,
		download.WithDigestAlgorithms(digestAlgorithms(policy)...),
	)
	if err != nil {
		return nil, fmt.Errorf("error downloading resource for digest processing: %w", err)
	}
	// The blob never leaves this function, so its file is released right away
	// instead of waiting for the caller or the cleanup to reclaim it.
	defer func() {
		if closeErr := data.Close(); closeErr != nil {
			slog.WarnContext(ctx, "failed to remove temporary file after digest processing", "err", closeErr)
		}
	}()

	// Source-side verification: if a policy applies, verify the downloaded
	// bytes against what the source advertises (RFC 9530 header, sibling URL,
	// …). This catches transport corruption and MITM before the digest is
	// recorded on the descriptor.
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
	// The pinned Value may be bare hex or go-digest form ("sha256:<hex>");
	// normalise both sides before comparison. Matches verifyProvidedDigest on
	// the input path.
	want := strings.TrimPrefix(resource.Digest.Value, "sha256:")
	if !strings.EqualFold(want, sha) {
		return nil, fmt.Errorf("digest mismatch: expected %s, got %s", resource.Digest.Value, sha)
	}

	return resource, nil
}

func (r *ResourceRepository) GetCredentialTypeScheme() *runtime.Scheme {
	return wgetcreds.Scheme
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
