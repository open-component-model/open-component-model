package repository

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	godigest "github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/repository/integrity"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/s3/internal/download"
	accessspec "ocm.software/open-component-model/bindings/go/s3/spec/access"
	v2 "ocm.software/open-component-model/bindings/go/s3/spec/access/v2"
	s3creds "ocm.software/open-component-model/bindings/go/s3/spec/credentials"
	identityv1 "ocm.software/open-component-model/bindings/go/s3/spec/identity/v1"
)

const (
	hashAlgorithmSHA256 = "SHA-256"
	genericBlobDigestV1 = "genericBlobDigest/v1"
)

var _ repository.ResourceRepository = (*ResourceRepository)(nil)

// ResourceRepository implements the ResourceRepository interface for the S3
// access type.
type ResourceRepository struct {
	maxDownloadSize *int64
	httpConfig      *httpv1alpha1.Config
	httpClient      *http.Client
}

// NewResourceRepository creates a new S3 resource repository.
func NewResourceRepository(opts ...Option) *ResourceRepository {
	options := &Options{}
	for _, opt := range opts {
		opt(options)
	}
	return &ResourceRepository{
		maxDownloadSize: options.MaxDownloadSize,
		httpConfig:      options.HTTPConfig,
		httpClient:      options.HTTPClient,
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

// DownloadResource returns the object at the bucket/key described by the S3 access
// spec as a lazy blob: every read requests the object and streams it through, nothing
// is stored. Reads after the first serve the object version the first one reported, if
// the store reports a real one; they are bound to ctx.
//
// The content is bound to the digest the resource declares, which is the digest over
// exactly these bytes; it is hashed while it is read, so an object that differs fails
// the read rather than this call.
func (r *ResourceRepository) DownloadResource(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (blob.ReadOnlyBlob, error) {
	spec, err := r.convertAccess(resource)
	if err != nil {
		return nil, err
	}

	b, err := integrity.Verify(ctx, resource.Digest, download.NewBlob(ctx, request(spec), r.options(credentials)...),
		integrity.WithLogAttrs(slog.Any("resource", resource.ToIdentity())))
	if err != nil {
		return nil, fmt.Errorf("resource %q: %w", resource.Name, err)
	}
	return b, nil
}

func (r *ResourceRepository) convertAccess(resource *descriptor.Resource) (*v2.S3, error) {
	if resource == nil {
		return nil, errors.New("resource is required")
	}
	if resource.Access == nil {
		return nil, errors.New("resource access is required")
	}

	spec, err := accessspec.ConvertToV2(resource.Access)
	if err != nil {
		return nil, fmt.Errorf("error converting resource access spec: %w", err)
	}
	if err := spec.Validate(); err != nil {
		return nil, fmt.Errorf("invalid s3 access spec: %w", err)
	}

	return spec, nil
}

// options returns the download options of the repository for credentials.
func (r *ResourceRepository) options(credentials runtime.Typed, extra ...download.Option) []download.Option {
	opts := []download.Option{download.WithCredentials(credentials)}
	if r.maxDownloadSize != nil {
		opts = append(opts, download.WithMaxDownloadSize(*r.maxDownloadSize))
	}
	if r.httpConfig != nil {
		opts = append(opts, download.WithHTTPConfig(r.httpConfig))
	}
	if r.httpClient != nil {
		opts = append(opts, download.WithHTTPClient(r.httpClient))
	}
	return append(opts, extra...)
}

func request(spec *v2.S3) download.Request {
	return download.Request{
		Region:       spec.Region,
		BucketName:   spec.BucketName,
		ObjectKey:    spec.ObjectKey,
		MediaType:    spec.MediaType,
		Version:      spec.Version,
		Endpoint:     spec.Endpoint,
		UsePathStyle: spec.UsePathStyle,
	}
}

// UploadResource is not supported by the S3 access type, which is
// download-only (matching ocmv1). It exists to satisfy the
// [repository.ResourceRepository] interface and always returns an error.
func (r *ResourceRepository) UploadResource(ctx context.Context, res *descriptor.Resource, content blob.ReadOnlyBlob, credentials runtime.Typed) (*descriptor.Resource, error) {
	return nil, errors.New("uploading resources is not supported by the S3 access type")
}

// GetResourceDigestProcessorCredentialConsumerIdentity resolves the credential consumer
// identity used when downloading the resource to compute its digest. It is the same identity
// used for a regular download.
func (r *ResourceRepository) GetResourceDigestProcessorCredentialConsumerIdentity(ctx context.Context, resource *descriptor.Resource) (runtime.Identity, error) {
	return r.GetResourceCredentialConsumerIdentity(ctx, resource)
}

// ProcessResourceDigest computes the digest of an S3 resource by streaming the
// referenced object and hashing it with SHA-256 on the way, which is the
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

	result, err := download.Stream(ctx, request(spec), io.Discard, r.options(credentials, download.WithDigestAlgorithm(godigest.SHA256))...)
	if err != nil {
		return nil, fmt.Errorf("error downloading resource for digest processing: %w", err)
	}
	resolvedValue := result.Digest.Encoded()

	resource = resource.DeepCopy()
	if resource.Digest == nil {
		resource.Digest = &descriptor.Digest{
			HashAlgorithm:          hashAlgorithmSHA256,
			NormalisationAlgorithm: genericBlobDigestV1,
			Value:                  resolvedValue,
		}
	} else {
		// A hand-written digest cannot know the normalisation algorithm and need not
		// restate the hash, so only a field pinned to a different algorithm is a conflict.
		// Spelling is not one either, hence the case-insensitive comparisons.
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

	switch pinned, served := pinningVersion(spec.Version), pinningVersion(result.VersionID); {
	case pinned != "":
		// The version is sent as the request's versionId, so a store answering with a
		// different one did not serve the object the access names. One reporting no
		// version cannot be checked and is taken at its word.
		if served != "" && served != pinned {
			return nil, fmt.Errorf("s3 object %s/%s was requested at version %q but the store served version %q",
				spec.BucketName, spec.ObjectKey, spec.Version, result.VersionID)
		}
	case served != "":
		spec.Version = served

		// The v2 descriptor encoder passes a [runtime.Raw] straight through but looks a
		// typed access up in its own scheme, where S3 is not registered.
		raw := &runtime.Raw{}
		if err := accessspec.Scheme.Convert(spec, raw); err != nil {
			return nil, fmt.Errorf("error encoding pinned s3 access: %w", err)
		}
		resource.Access = raw
	default:
		// Logged rather than rejected: an unpinned access risks availability rather than
		// integrity, and erroring would make digests unusable for every unversioned bucket.
		slog.WarnContext(ctx, "s3 object carries no version, so its access cannot be pinned to the digested content and may later resolve to a different object",
			slog.String("bucket", spec.BucketName),
			slog.String("objectKey", spec.ObjectKey))
	}

	return resource, nil
}

// pinningVersion returns versionID if it names an object version, otherwise empty.
func pinningVersion(versionID string) string {
	if !download.IsVersion(versionID) {
		return ""
	}
	return versionID
}

func (r *ResourceRepository) GetCredentialTypeScheme() *runtime.Scheme {
	return s3creds.Scheme
}
