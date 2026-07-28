package repository

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"

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

// ResourceRepository implements the ResourceRepository interface for the S3 access type.
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
	if resource == nil {
		return nil, fmt.Errorf("resource is required")
	}
	if resource.Access == nil {
		return nil, fmt.Errorf("resource access is required")
	}

	spec := v1.S3Bucket{}
	if err := r.GetResourceRepositoryScheme().Convert(resource.Access, &spec); err != nil {
		return nil, fmt.Errorf("error converting resource access spec: %w", err)
	}

	return identityv1.IdentityFromObject(spec.BucketName, spec.ObjectKey, spec.Endpoint)
}

// DownloadResource downloads a resource from the bucket/key described by the S3 access spec.
//
// The object is streamed into a file under the configured TempFolder, and the
// returned blob reads from that file. The file outlives this call and nothing
// removes it afterwards, so the caller owns it.
func (r *ResourceRepository) DownloadResource(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (blob.ReadOnlyBlob, error) {
	spec, err := r.convertAccess(resource)
	if err != nil {
		return nil, err
	}

	result, err := r.download(ctx, spec, credentials, r.filesystemConfig.TempFolder)
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

// download streams the object described by spec into tempDir.
func (r *ResourceRepository) download(ctx context.Context, spec *v1.S3Bucket, credentials runtime.Typed, tempDir string) (*download.Result, error) {
	opts := []download.Option{
		download.WithCredentials(credentials),
		download.WithTempDir(tempDir),
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

// UploadResource is not supported by the S3 access type, which is download-only
// (matching ocmv1). It exists to satisfy the [repository.ResourceRepository]
// interface and always returns an error.
func (r *ResourceRepository) UploadResource(ctx context.Context, res *descriptor.Resource, content blob.ReadOnlyBlob, credentials runtime.Typed) (*descriptor.Resource, error) {
	return nil, fmt.Errorf("uploading resources is not supported by the s3 access type")
}

// GetResourceDigestProcessorCredentialConsumerIdentity resolves the credential consumer
// identity used when downloading the resource to compute its digest. It is the same identity
// used for a regular download.
func (r *ResourceRepository) GetResourceDigestProcessorCredentialConsumerIdentity(ctx context.Context, resource *descriptor.Resource) (runtime.Identity, error) {
	return r.GetResourceCredentialConsumerIdentity(ctx, resource)
}

// ProcessResourceDigest computes the digest of an S3 resource by downloading the
// referenced object and hashing it with SHA-256, which is the source of truth rather
// than the S3 ETag. When the resource already carries a digest, the computed value is
// verified against it.
//
// After a successful digest, the access is pinned to the object version that was read;
// see [ResourceRepository.pinAccess].
func (r *ResourceRepository) ProcessResourceDigest(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (*descriptor.Resource, error) {
	spec, err := r.convertAccess(resource)
	if err != nil {
		return nil, err
	}

	// The blob never leaves this function, so unlike DownloadResource this owns the
	// downloaded file and removes it.
	tempDir, err := os.MkdirTemp(r.filesystemConfig.TempFolder, "ocm-s3-digest-*")
	if err != nil {
		return nil, fmt.Errorf("error creating temporary directory for digest processing: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(tempDir)
	}()

	result, err := r.download(ctx, spec, credentials, tempDir)
	if err != nil {
		return nil, fmt.Errorf("error downloading resource for digest processing: %w", err)
	}

	rc, err := result.Blob.ReadCloser()
	if err != nil {
		return nil, fmt.Errorf("error opening downloaded resource: %w", err)
	}
	defer func() { _ = rc.Close() }()

	dig, err := godigest.FromReader(rc)
	if err != nil {
		return nil, fmt.Errorf("error computing resource digest: %w", err)
	}

	resource = resource.DeepCopy()
	if resource.Digest == nil {
		resource.Digest = &descriptor.Digest{
			HashAlgorithm:          hashAlgorithmSHA256,
			NormalisationAlgorithm: genericBlobDigestV1,
			Value:                  dig.Encoded(),
		}
		r.pinAccess(ctx, resource, spec, result.VersionID)
		return resource, nil
	}

	if resource.Digest.HashAlgorithm != hashAlgorithmSHA256 {
		return nil, fmt.Errorf("unsupported hash algorithm: expected %s, got %s", hashAlgorithmSHA256, resource.Digest.HashAlgorithm)
	}
	if resource.Digest.NormalisationAlgorithm != genericBlobDigestV1 {
		return nil, fmt.Errorf("unsupported normalisation algorithm: expected %s, got %s", genericBlobDigestV1, resource.Digest.NormalisationAlgorithm)
	}
	if resource.Digest.Value != dig.Encoded() {
		return nil, fmt.Errorf("digest mismatch: expected %s, got %s", resource.Digest.Value, dig.Encoded())
	}

	r.pinAccess(ctx, resource, spec, result.VersionID)

	return resource, nil
}

// pinAccess points the resource's access at the object version the digest was computed
// over, satisfying the [repository.ResourceDigestProcessor] requirement that a processed
// access "MUST always reference the content described by the digest and cannot be mutated".
// An access that already names a version was pinned by whoever wrote it and is left alone.
//
// Unversioned buckets report no version, or the placeholder
// [download.UnversionedVersionID], neither of which survives an overwrite. Those are
// logged rather than pinned or rejected, because erroring would make digests unusable
// for every unversioned bucket.
func (r *ResourceRepository) pinAccess(ctx context.Context, resource *descriptor.Resource, spec *v1.S3Bucket, versionID string) {
	if spec.Version != "" {
		return
	}

	if versionID == "" || versionID == download.UnversionedVersionID {
		slog.WarnContext(ctx, "s3 object carries no version, so its access cannot be pinned to the digested content and may later resolve to a different object",
			slog.String("bucket", spec.BucketName),
			slog.String("objectKey", spec.ObjectKey))
		return
	}

	spec.Version = versionID
	resource.Access = spec
}
