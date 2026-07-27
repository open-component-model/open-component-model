package repository

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path"
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
)

const (
	// hashAlgorithmSHA256 is the hash algorithm used for s3 resource digests.
	hashAlgorithmSHA256 = "SHA-256"
	// genericBlobDigestV1 is the normalisation algorithm for a plain downloaded blob.
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

// GetResourceCredentialConsumerIdentity resolves the credential consumer identity for the given resource.
//
// The object path (bucket/objectKey) is always encoded as the path attribute. The
// hostname is set only for a custom S3-compatible endpoint, where it identifies where
// credentials apply. For plain AWS S3 (no endpoint) the identity carries no hostname:
// AWS is the default target, so credentials resolve host-agnostically and a config does
// not need to name the AWS host. The default matcher requires equal hostnames, so a
// hostname is deliberately omitted rather than defaulted to the AWS host.
func (r *ResourceRepository) GetResourceCredentialConsumerIdentity(ctx context.Context, resource *descriptor.Resource) (runtime.Identity, error) {
	if resource == nil {
		return nil, fmt.Errorf("resource is required")
	}
	if resource.Access == nil {
		return nil, fmt.Errorf("resource access is required")
	}

	spec := v1.S3{}
	if err := r.GetResourceRepositoryScheme().Convert(resource.Access, &spec); err != nil {
		return nil, fmt.Errorf("error converting resource access spec: %w", err)
	}

	if spec.BucketName == "" {
		return nil, fmt.Errorf("bucketName is required")
	}

	loc := path.Join(spec.BucketName, spec.ObjectKey)

	var identity runtime.Identity
	if spec.Endpoint != "" {
		id, err := runtime.ParseURLToIdentity(strings.TrimSuffix(spec.Endpoint, "/") + "/" + loc)
		if err != nil {
			return nil, fmt.Errorf("error parsing s3 endpoint to identity: %w", err)
		}
		identity = id
	} else {
		identity = runtime.Identity{
			runtime.IdentityAttributePath: loc,
		}
	}

	identity.SetType(runtime.NewUnversionedType(accessspec.S3BucketConsumerType))

	return identity, nil
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

	// An empty TempFolder is a valid config and selects the OS default.
	result, err := r.download(ctx, spec, credentials, r.filesystemConfig.TempFolder)
	if err != nil {
		return nil, err
	}

	return result.Blob, nil
}

// convertAccess resolves the resource's access into the typed S3 access spec.
func (r *ResourceRepository) convertAccess(resource *descriptor.Resource) (*v1.S3, error) {
	if resource == nil {
		return nil, fmt.Errorf("resource is required")
	}
	if resource.Access == nil {
		return nil, fmt.Errorf("resource access is required")
	}

	spec := &v1.S3{}
	if err := accessspec.Scheme.Convert(resource.Access, spec); err != nil {
		return nil, fmt.Errorf("error converting resource access spec: %w", err)
	}

	return spec, nil
}

// download streams the object described by spec into tempDir and returns a blob
// backed by the resulting file, along with the object version that was read.
func (r *ResourceRepository) download(ctx context.Context, spec *v1.S3, credentials runtime.Typed, tempDir string) (*download.Result, error) {
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

// ProcessResourceDigest computes the digest of an S3 resource by downloading the referenced
// object and hashing it. When the resource already carries a digest, the computed value is
// verified against it. OCM's own SHA-256 over the content is the source of truth, not the S3 ETag.
//
// After a successful digest, the access is pinned to the object version that was read, so it
// keeps referencing the content the digest describes. See [ResourceRepository.pinAccess] for
// the case where S3 offers no version to pin to.
func (r *ResourceRepository) ProcessResourceDigest(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (*descriptor.Resource, error) {
	spec, err := r.convertAccess(resource)
	if err != nil {
		return nil, err
	}

	// The object is only read to hash it and the blob never leaves this function,
	// so unlike DownloadResource this owns the downloaded file and removes it.
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

// pinAccess points the resource's access at the exact object version the digest was
// computed over, so the access cannot later resolve to different content. This is the
// [repository.ResourceDigestProcessor] requirement that a processed access "MUST always
// reference the content described by the digest and cannot be mutated".
//
// An access that already names a version is left alone: it was pinned by whoever wrote
// it, and the download honoured it.
//
// Not every object can be pinned. A bucket without versioning reports no version, or
// the placeholder [download.UnversionedVersionID], neither of which survives an
// overwrite. Rather than record a version that pins nothing, the access is left as it
// is and the gap is logged: erroring instead would make digests unusable for every
// unversioned bucket, which is a bigger break than the one it would prevent.
func (r *ResourceRepository) pinAccess(ctx context.Context, resource *descriptor.Resource, spec *v1.S3, versionID string) {
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
