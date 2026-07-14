package repository

import (
	"context"
	"fmt"
	"path"
	"strings"

	godigest "github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
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

	// awsDefaultHost is used as the consumer identity host when no custom endpoint
	// is configured (AWS S3).
	awsDefaultHost = "s3.amazonaws.com"
)

var _ repository.ResourceRepository = (*ResourceRepository)(nil)

// ResourceRepository implements the ResourceRepository interface for the S3 access type.
type ResourceRepository struct {
	client          download.ObjectGetter
	maxDownloadSize *int64
}

// NewResourceRepository creates a new S3 resource repository.
func NewResourceRepository(opts ...Option) *ResourceRepository {
	options := &Options{}
	for _, opt := range opts {
		opt(options)
	}
	return &ResourceRepository{
		client:          options.Client,
		maxDownloadSize: options.MaxDownloadSize,
	}
}

// GetResourceRepositoryScheme returns the scheme used by the S3 resource repository.
func (r *ResourceRepository) GetResourceRepositoryScheme() *runtime.Scheme {
	return accessspec.Scheme
}

// GetResourceCredentialConsumerIdentity resolves the credential consumer identity for the given resource.
// The identity is keyed on the endpoint host (or the AWS default host) and the object path
// (bucket/objectKey), so credentials can be scoped per endpoint and, optionally, per bucket or key
// prefix. The bucket/objectKey is encoded as the path attribute, which the default identity matcher
// glob-matches and treats as optional: a credential config that omits the path still matches every
// bucket, while one that sets it (e.g. my-bucket or my-bucket/*) scopes the credentials down.
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

	identity, err := runtime.ParseURLToIdentity(s3BaseURL(&spec))
	if err != nil {
		return nil, fmt.Errorf("error parsing s3 location to identity: %w", err)
	}

	identity.SetType(runtime.NewUnversionedType(accessspec.S3BucketConsumerType))

	return identity, nil
}

// s3BaseURL builds the URL that identifies the object for credential resolution:
// the endpoint (or the AWS default host) followed by bucket/objectKey. Parsing it
// with [runtime.ParseURLToIdentity] yields the scheme, host, port and a path of
// bucket/objectKey.
func s3BaseURL(spec *v1.S3) string {
	base := "https://" + awsDefaultHost
	if spec.Endpoint != "" {
		base = strings.TrimSuffix(spec.Endpoint, "/")
	}
	loc := path.Join(spec.BucketName, spec.ObjectKey)
	return base + "/" + loc
}

// DownloadResource downloads a resource from the bucket/key described by the S3 access spec.
func (r *ResourceRepository) DownloadResource(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (blob.ReadOnlyBlob, error) {
	if resource == nil {
		return nil, fmt.Errorf("resource is required")
	}
	if resource.Access == nil {
		return nil, fmt.Errorf("resource access is required")
	}

	spec := v1.S3{}
	if err := accessspec.Scheme.Convert(resource.Access, &spec); err != nil {
		return nil, fmt.Errorf("error converting resource access spec: %w", err)
	}

	opts := []download.Option{download.WithCredentials(credentials)}
	if r.client != nil {
		opts = append(opts, download.WithClient(r.client))
	}
	if r.maxDownloadSize != nil {
		opts = append(opts, download.WithMaxDownloadSize(*r.maxDownloadSize))
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
func (r *ResourceRepository) ProcessResourceDigest(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (*descriptor.Resource, error) {
	data, err := r.DownloadResource(ctx, resource, credentials)
	if err != nil {
		return nil, fmt.Errorf("error downloading resource for digest processing: %w", err)
	}

	rc, err := data.ReadCloser()
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

	return resource, nil
}
