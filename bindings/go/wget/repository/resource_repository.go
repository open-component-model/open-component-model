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
	hashAlgorithmSHA256 = "SHA-256"
	// genericBlobDigestV1 is the normalisation algorithm for a plain
	// downloaded blob.
	genericBlobDigestV1 = "genericBlobDigest/v1"
)

var _ repository.ResourceRepository = (*ResourceRepository)(nil)

// ResourceRepository implements repository.ResourceRepository for wget access
// types.
type ResourceRepository struct {
	client           *http.Client
	maxDownloadSize  int64
	filesystemConfig *filesystemv1alpha1.Config
	// checksumConfig steers the digest processor's [checksumhttpv1alpha1.ChecksumMode].
	// Nil means "compute from stream without external verification".
	checksumConfig *checksumhttpv1alpha1.Config
}

// NewResourceRepository builds a wget resource repository. filesystemConfig's
// TempFolder, when set, is used for downloaded body files.
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
		checksumConfig:   options.ChecksumConfig,
	}
}

func (r *ResourceRepository) GetResourceRepositoryScheme() *runtime.Scheme {
	return accessspec.Scheme
}

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

// DownloadResource downloads the resource. The returned blob owns a temp file
// callers should close; unclosed blobs have their file removed on GC.
func (r *ResourceRepository) DownloadResource(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (blob.ReadOnlyBlob, error) {
	b, _, err := r.download(ctx, resource, credentials)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// download streams the resource body into the temp folder and returns the
// file-backed blob plus the decoded access spec. Extra options are appended
// after the repository defaults so callers (notably [ProcessResourceDigest])
// can hash the stream inline via [download.WithDigestAlgorithms].
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

// GetResourceDigestProcessorCredentialConsumerIdentity reuses the download
// identity so credentials apply to both paths.
func (r *ResourceRepository) GetResourceDigestProcessorCredentialConsumerIdentity(ctx context.Context, resource *descriptor.Resource) (runtime.Identity, error) {
	return r.GetResourceCredentialConsumerIdentity(ctx, resource)
}

// ProcessResourceDigest establishes a wget access resource's digest.
//
// A Wget/v1 access references remote bytes, so OCM can pin the digest from
// what the source advertises in its response headers (RFC 9530 Content-Digest,
// x-checksum-*) via a single HEAD, without fetching the body.
//
// Modes (resolved from the checksum-http config for the access URL):
//   - PeekWithHEADOrFail — HEAD-pin; abort if nothing advertised.
//   - PeekWithHEADOrCompute — HEAD-pin; else download the body and hash SHA-256.
//   - Compute — skip the HEAD; always download the body and hash SHA-256.
//   - Disable — establish no digest; return the resource unchanged.
//
// A pinned Digest is honoured: it must agree with the source-advertised digest
// (fast path) or the computed SHA-256 (download path). Pinned and policy are
// each checked against the same authority, never against each other.
func (r *ResourceRepository) ProcessResourceDigest(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (*descriptor.Resource, error) {
	url := policyURL(resource)
	mode := r.checksumConfig.ModeForURL(url)

	switch mode {
	case checksumhttpv1alpha1.ChecksumModeDisable:
		// Verification off: leave the resource (and any pinned digest) untouched.
		slog.DebugContext(ctx, "wget: checksum processing disabled", "url", url)
		return resource, nil
	case checksumhttpv1alpha1.ChecksumModePeekWithHEADOrFail, checksumhttpv1alpha1.ChecksumModePeekWithHEADOrCompute:
		policy := checksum.Policy{Sources: checksum.BuiltinSources(), OnMissing: checksum.Compute}
		if mode == checksumhttpv1alpha1.ChecksumModePeekWithHEADOrFail {
			policy.OnMissing = checksum.Fail
		}
		result, done, err := r.processDigestViaPeek(ctx, resource, credentials, policy)
		if err != nil {
			return nil, err
		}
		if done {
			return result, nil
		}
		slog.DebugContext(ctx, "wget: access fast path yielded nothing; falling back to download-and-hash",
			"url", url, "onMissing", policy.OnMissing)
	case checksumhttpv1alpha1.ChecksumModeCompute:
		// Skip the HEAD fast path entirely; always download and hash.
	}

	// Download-and-hash path (Compute, or a peek mode that found nothing under
	// onMissing=compute). SHA-256 is the only algorithm required here.
	data, wget, err := r.download(ctx, resource, credentials,
		download.WithDigestAlgorithms(download.DigestAlgorithm{
			Name: checksum.StorageAlgorithm.OCMName,
			New:  checksum.StorageAlgorithm.New,
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("error downloading resource for digest processing: %w", err)
	}
	defer func() {
		if closeErr := data.Close(); closeErr != nil {
			slog.WarnContext(ctx, "failed to remove temporary file after digest processing", "err", closeErr)
		}
	}()

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
		slog.InfoContext(ctx, "wget: digest recorded from downloaded bytes",
			"url", wget.URL, "algorithm", hashAlgorithmSHA256, "value", sha)
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
	slog.InfoContext(ctx, "wget: pinned digest matches downloaded bytes",
		"url", wget.URL, "algorithm", hashAlgorithmSHA256, "value", sha)

	return resource, nil
}

func (r *ResourceRepository) GetCredentialTypeScheme() *runtime.Scheme {
	return wgetcreds.Scheme
}

// processDigestViaPeek runs the no-download fast path. done=true means the
// digest is established; done=false signals "no advertised digest, fall back
// to download" (permitted only under onMissing=compute).
func (r *ResourceRepository) processDigestViaPeek(
	ctx context.Context,
	resource *descriptor.Resource,
	credentials runtime.Typed,
	policy checksum.Policy,
) (*descriptor.Resource, bool, error) {
	url := policyURL(resource)
	// The reduced configuration accepts every supported algorithm, strongest
	// first; there is no operator-facing preference knob.
	prefer := checksum.All
	names := make([]string, 0, len(prefer))
	for _, alg := range prefer {
		names = append(names, alg.OCMName)
	}
	slog.DebugContext(ctx, "wget: peeking source-advertised checksum", "url", url, "prefer", names)

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
	slog.DebugContext(ctx, "wget: source advertised digest",
		"url", url, "algorithm", exp.Algorithm.OCMName, "value", exp.Value)

	out := resource.DeepCopy()
	if out.Digest != nil {
		// A pinned digest must match the same algorithm the source advertises:
		// the operator has explicitly asked us not to hash the body.
		if !strings.EqualFold(out.Digest.HashAlgorithm, exp.Algorithm.OCMName) {
			return nil, false, fmt.Errorf("pinned digest algorithm %q does not match the source-advertised algorithm %q", out.Digest.HashAlgorithm, exp.Algorithm.OCMName)
		}
		if out.Digest.NormalisationAlgorithm != "" && out.Digest.NormalisationAlgorithm != genericBlobDigestV1 {
			return nil, false, fmt.Errorf("unsupported normalisation algorithm: expected %s, got %s", genericBlobDigestV1, out.Digest.NormalisationAlgorithm)
		}
		// Pinned value may be bare hex or go-digest form ("sha256:<hex>").
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
	slog.InfoContext(ctx, "wget: digest pinned from source-advertised checksum",
		"url", url, "algorithm", exp.Algorithm.OCMName, "value", exp.Value)
	return out, true, nil
}

// policyURL returns the URL used for wget-config host matching, or "" for a
// missing or non-wget access.
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
