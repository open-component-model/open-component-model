package repository

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	godigest "github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob"
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/s3/internal/download"
	accessspec "ocm.software/open-component-model/bindings/go/s3/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/s3/spec/access/v1"
	identityv1 "ocm.software/open-component-model/bindings/go/s3/spec/identity/v1"
)

const (
	hashAlgorithmSHA256 = "SHA-256"
	genericBlobDigestV1 = "genericBlobDigest/v1"
)

var _ repository.ResourceRepository = (*ResourceRepository)(nil)

// ResourceRepository implements the ResourceRepository interface for the S3Bucket
// access type.
type ResourceRepository struct {
	client           download.ObjectGetter
	maxDownloadSize  *int64
	httpConfig       *httpv1alpha1.Config
	httpClient       *http.Client
	filesystemConfig *filesystemv1alpha1.Config
}

// NewResourceRepository creates a new S3 resource repository. If filesystemConfig
// is non-nil, its TempFolder is used for the files downloaded objects are streamed
// into; otherwise os.CreateTemp's default directory is used.
func NewResourceRepository(filesystemConfig *filesystemv1alpha1.Config, opts ...Option) *ResourceRepository {
	if filesystemConfig == nil {
		filesystemConfig = &filesystemv1alpha1.Config{}
	}
	options := &Options{}
	for _, opt := range opts {
		opt(options)
	}
	return &ResourceRepository{
		client:           options.client,
		maxDownloadSize:  options.MaxDownloadSize,
		httpConfig:       options.HTTPConfig,
		httpClient:       options.HTTPClient,
		filesystemConfig: filesystemConfig,
	}
}

// GetResourceRepositoryScheme returns the scheme used by the S3 resource repository.
func (r *ResourceRepository) GetResourceRepositoryScheme() *runtime.Scheme {
	return accessspec.Scheme
}

// GetResourceCredentialConsumerIdentity resolves the credential consumer identity
// for the given resource. It always carries the object path, and a hostname only for
// a custom endpoint; see the package documentation of the s3 module for the full
// matching rules.
func (r *ResourceRepository) GetResourceCredentialConsumerIdentity(ctx context.Context, resource *descriptor.Resource) (runtime.Identity, error) {
	spec, err := r.convertAccess(resource)
	if err != nil {
		return nil, err
	}

	return identityv1.IdentityFromObject(spec.BucketName, spec.ObjectKey, spec.Endpoint)
}

// DownloadResource downloads a resource from the bucket/key described by the
// S3Bucket access spec.
//
// The object is streamed into a file under the configured TempFolder, and the
// returned blob reads from that file, which outlives this call. The blob owns that
// file: callers should close it (it implements io.Closer) once they are done, and an
// unclosed blob has its file removed when it becomes unreachable.
func (r *ResourceRepository) DownloadResource(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (blob.ReadOnlyBlob, error) {
	spec, err := r.convertAccess(resource)
	if err != nil {
		return nil, err
	}

	result, err := r.download(ctx, spec, credentials)
	if err != nil {
		return nil, err
	}

	return result.Blob, nil
}

func (r *ResourceRepository) convertAccess(resource *descriptor.Resource) (*v1.S3Bucket, error) {
	if resource == nil {
		return nil, fmt.Errorf("resource is required")
	}
	if resource.Access == nil {
		return nil, fmt.Errorf("resource access is required")
	}

	spec := &v1.S3Bucket{}
	if err := accessspec.Scheme.Convert(resource.Access, spec); err != nil {
		return nil, fmt.Errorf("error converting resource access spec: %w", err)
	}

	return spec, nil
}

// download streams the object described by spec into the configured temp folder and
// returns it as a file-backed blob owning that file.
func (r *ResourceRepository) download(ctx context.Context, spec *v1.S3Bucket, credentials runtime.Typed) (*download.Result, error) {
	opts := []download.Option{
		download.WithCredentials(credentials),
		download.WithTempDir(r.filesystemConfig.TempFolder),
	}
	if r.client != nil {
		opts = append(opts, download.WithClient(r.client))
	}
	if r.maxDownloadSize != nil {
		opts = append(opts, download.WithMaxDownloadSize(*r.maxDownloadSize))
	}
	if r.httpConfig != nil {
		opts = append(opts, download.WithHTTPConfig(r.httpConfig))
	}
	if r.httpClient != nil {
		opts = append(opts, download.WithHTTPClient(r.httpClient))
	}

	return download.Download(ctx, download.Request{
		Region:                spec.Region,
		BucketName:            spec.BucketName,
		ObjectKey:             spec.ObjectKey,
		MediaType:             spec.MediaType,
		Version:               spec.Version,
		Endpoint:              spec.Endpoint,
		UsePathStyle:          spec.UsePathStyle,
		InsecureSkipTLSVerify: spec.InsecureSkipTLSVerify,
	}, opts...)
}

// UploadResource is not supported by the S3Bucket access type, which is
// download-only (matching ocmv1). It exists to satisfy the
// [repository.ResourceRepository] interface and always returns an error.
func (r *ResourceRepository) UploadResource(ctx context.Context, res *descriptor.Resource, content blob.ReadOnlyBlob, credentials runtime.Typed) (*descriptor.Resource, error) {
	return nil, fmt.Errorf("uploading resources is not supported by the S3Bucket access type")
}

// GetResourceDigestProcessorCredentialConsumerIdentity resolves the credential consumer
// identity used when downloading the resource to compute its digest. It is the same identity
// used for a regular download.
func (r *ResourceRepository) GetResourceDigestProcessorCredentialConsumerIdentity(ctx context.Context, resource *descriptor.Resource) (runtime.Identity, error) {
	return r.GetResourceCredentialConsumerIdentity(ctx, resource)
}

// ProcessResourceDigest computes the digest of an S3 resource by downloading the
// referenced object and taking the SHA-256 the downloaded blob carries, which is the
// source of truth rather than the S3 ETag. When the resource already carries a digest,
// the computed value is verified against it.
//
// After a successful digest, the access is pinned to the object version that was read;
// see [ResourceRepository.pinAccess].
func (r *ResourceRepository) ProcessResourceDigest(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (*descriptor.Resource, error) {
	spec, err := r.convertAccess(resource)
	if err != nil {
		return nil, err
	}

	result, err := r.download(ctx, spec, credentials)
	if err != nil {
		return nil, fmt.Errorf("error downloading resource for digest processing: %w", err)
	}
	// The blob never leaves this function, so its file is released right away instead
	// of waiting for the caller or the cleanup to reclaim it.
	defer func() {
		if closeErr := result.Blob.Close(); closeErr != nil {
			slog.WarnContext(ctx, "failed to remove temporary file after digest processing", "err", closeErr)
		}
	}()

	raw, _ := result.Blob.Digest()
	resolved, err := godigest.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("downloaded s3 object does not carry a valid digest: %w", err)
	}
	resolvedValue := resolved.Encoded()

	resource = resource.DeepCopy()
	if resource.Digest == nil {
		resource.Digest = &descriptor.Digest{
			HashAlgorithm:          hashAlgorithmSHA256,
			NormalisationAlgorithm: genericBlobDigestV1,
			Value:                  resolvedValue,
		}
	} else {
		// A hand-written resource.Digest in a component constructor cannot know the
		// normalisation algorithm — the blob is never normalised — and need not restate
		// the hash. An unset field means "fill it in with what we compute"; only a field
		// pinned to a *different* algorithm is a genuine conflict. Spelling is not a
		// conflict either: comparisons ignore case, so "sha-256" or an uppercase hex value
		// verifies instead of failing while an absent field would have been accepted.
		if resource.Digest.HashAlgorithm != "" && !strings.EqualFold(resource.Digest.HashAlgorithm, hashAlgorithmSHA256) {
			return nil, fmt.Errorf("hash algorithm mismatch: expected %s, got %s", hashAlgorithmSHA256, resource.Digest.HashAlgorithm)
		}
		if resource.Digest.NormalisationAlgorithm != "" && !strings.EqualFold(resource.Digest.NormalisationAlgorithm, genericBlobDigestV1) {
			return nil, fmt.Errorf("normalisation algorithm mismatch: expected %s, got %s", genericBlobDigestV1, resource.Digest.NormalisationAlgorithm)
		}
		if !strings.EqualFold(resource.Digest.Value, resolvedValue) {
			return nil, fmt.Errorf("digest value mismatch: expected %s, got %s", resource.Digest.Value, resolvedValue)
		}

		// Canonicalize the accepted spellings so descriptors do not vary by author.
		resource.Digest.HashAlgorithm = hashAlgorithmSHA256
		resource.Digest.NormalisationAlgorithm = genericBlobDigestV1
		resource.Digest.Value = resolvedValue
	}

	// Pinning the access to the version that was read satisfies the
	// [repository.ResourceDigestProcessor] requirement that a processed access "MUST
	// always reference the content described by the digest and cannot be mutated".
	// See "Object versions and unversioned buckets" in the package documentation of
	// the s3 module for what an unpinned access does and does not risk.
	switch pinned, served := pinningVersion(spec.Version), pinningVersion(result.VersionID); {
	case pinned != "":
		// The version is sent as the versionId of the request, so a store answering with
		// a different one did not serve the object the access names, and the digest would
		// describe content found at neither version. A store reporting no version at all
		// cannot be checked and is taken at its word.
		if served != "" && served != pinned {
			return nil, fmt.Errorf("s3 object %s/%s was requested at version %q but the store served version %q",
				spec.BucketName, spec.ObjectKey, spec.Version, result.VersionID)
		}
	case served != "":
		spec.Version = served

		// Hand the pinned access back in the raw form a constructor parsed. The v2
		// descriptor encoder passes a [runtime.Raw] straight through but has to look a
		// typed access up in its own scheme, where S3Bucket is not registered.
		raw := &runtime.Raw{}
		if err := accessspec.Scheme.Convert(spec, raw); err != nil {
			return nil, fmt.Errorf("error encoding pinned s3 access: %w", err)
		}
		resource.Access = raw
	default:
		// Logged rather than rejected: an unpinned access risks availability rather than
		// integrity, and erroring would make digests unusable for every unversioned
		// bucket — which is the default on AWS.
		slog.WarnContext(ctx, "s3 object carries no version, so its access cannot be pinned to the digested content and may later resolve to a different object",
			slog.String("bucket", spec.BucketName),
			slog.String("objectKey", spec.ObjectKey))
	}

	return resource, nil
}

// pinningVersion returns versionID when it identifies immutable content, and the empty
// string when it does not. An unversioned bucket reports either no version at all or the
// placeholder [download.UnversionedVersionID], and neither survives an overwrite — which
// holds whether the value was reported by the store or written by the author, so both are
// screened through here.
func pinningVersion(versionID string) string {
	if versionID == download.UnversionedVersionID {
		return ""
	}
	return versionID
}
