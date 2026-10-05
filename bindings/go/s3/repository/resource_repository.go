package repository

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
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
// S3 access spec.
//
// The object is streamed into a file under the configured TempFolder, and the
// returned blob reads from that file, which outlives this call and is owned by the
// caller.
//
// The content is held to the digest the resource declares, which is the digest over
// exactly these bytes, so a store serving something else fails the read.
func (r *ResourceRepository) DownloadResource(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (blob.ReadOnlyBlob, error) {
	spec, err := r.convertAccess(resource)
	if err != nil {
		return nil, err
	}

	var tempFolder string
	if r.filesystemConfig.TempFolder != nil {
		tempFolder = *r.filesystemConfig.TempFolder
	}

	result, err := r.download(ctx, spec, credentials, tempFolder)
	if err != nil {
		return nil, err
	}

	return repository.VerifyDownload(ctx, resource, result.Blob)
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

// download streams the object described by spec into tempDir and returns it as a
// file-backed blob. The file outlives this call and is owned by the caller.
func (r *ResourceRepository) download(ctx context.Context, spec *v2.S3, credentials runtime.Typed, tempDir string) (*download.Result, error) {
	opts := append(r.clientOptions(credentials), download.WithTempDir(tempDir))
	if r.maxDownloadSize != nil {
		opts = append(opts, download.WithMaxDownloadSize(*r.maxDownloadSize))
	}

	return download.Download(ctx, request(spec), opts...)
}

// clientOptions are the download options that shape the S3 client.
func (r *ResourceRepository) clientOptions(credentials runtime.Typed) []download.Option {
	opts := []download.Option{download.WithCredentials(credentials)}
	if r.httpConfig != nil {
		opts = append(opts, download.WithHTTPConfig(r.httpConfig))
	}
	if r.httpClient != nil {
		opts = append(opts, download.WithHTTPClient(r.httpClient))
	}
	return opts
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

// ProcessResourceDigest computes the SHA-256 digest of an S3 resource, which is the
// source of truth rather than the S3 ETag. When the resource already carries a digest,
// the computed value is verified against it.
//
// A store that keeps a SHA-256 checksum of the whole object answers from a HeadObject,
// so the object is not transferred; see [ResourceRepository.resolveDigest].
//
// After a successful digest, the access is pinned to the object version that was read;
// see [ResourceRepository.pinAccess].
func (r *ResourceRepository) ProcessResourceDigest(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (*descriptor.Resource, error) {
	spec, err := r.convertAccess(resource)
	if err != nil {
		return nil, err
	}

	resolvedValue, versionID, err := r.resolveDigest(ctx, spec, credentials)
	if err != nil {
		return nil, err
	}

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

	switch pinned, served := pinningVersion(spec.Version), pinningVersion(versionID); {
	case pinned != "":
		// The version is sent as the request's versionId, so a store answering with a
		// different one did not serve the object the access names. One reporting no
		// version cannot be checked and is taken at its word.
		if served != "" && served != pinned {
			return nil, fmt.Errorf("s3 object %s/%s was requested at version %q but the store served version %q",
				spec.BucketName, spec.ObjectKey, spec.Version, versionID)
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

// resolveDigest returns the hex SHA-256 of the object and the version it was taken at.
//
// It first asks the store with a HeadObject: a SHA-256 checksum covering the whole
// object, which S3 verifies on upload and keeps for single-part uploads made with it,
// is the digest without transferring a byte. Everything else — no checksum, another
// algorithm, a multipart COMPOSITE checksum, a store not answering HEAD — falls back to
// downloading and hashing. Taking the store's word costs no integrity: every later
// download is verified against the digest this produces.
func (r *ResourceRepository) resolveDigest(ctx context.Context, spec *v2.S3, credentials runtime.Typed) (string, string, error) {
	info, err := download.Head(ctx, request(spec), r.clientOptions(credentials)...)
	switch {
	case err != nil:
		slog.DebugContext(ctx, "s3 HeadObject failed, digesting the object by download",
			slog.String("bucket", spec.BucketName), slog.String("objectKey", spec.ObjectKey), slog.String("err", err.Error()))
	case info.SHA256 != "":
		slog.DebugContext(ctx, "s3 object digest taken from the store's SHA-256 checksum",
			slog.String("bucket", spec.BucketName), slog.String("objectKey", spec.ObjectKey))
		return info.SHA256, info.VersionID, nil
	}

	return r.digestByDownload(ctx, spec, credentials)
}

// digestByDownload downloads the object into a temporary directory it removes again
// and returns the SHA-256 computed while streaming, with the version that was read.
func (r *ResourceRepository) digestByDownload(ctx context.Context, spec *v2.S3, credentials runtime.Typed) (string, string, error) {
	tempFolder := ""
	if r.filesystemConfig.TempFolder != nil {
		tempFolder = *r.filesystemConfig.TempFolder
	}

	tempDir, err := os.MkdirTemp(tempFolder, "ocm-s3-digest-*")
	if err != nil {
		return "", "", fmt.Errorf("error creating temporary directory for digest processing: %w", err)
	}
	defer func() {
		if rmErr := os.RemoveAll(tempDir); rmErr != nil {
			slog.WarnContext(ctx, "failed to remove temporary directory after digest processing", "path", tempDir, "err", rmErr)
		}
	}()

	result, err := r.download(ctx, spec, credentials, tempDir)
	if err != nil {
		return "", "", fmt.Errorf("error downloading resource for digest processing: %w", err)
	}

	raw, ok := result.Blob.Digest()
	if !ok {
		return "", "", fmt.Errorf("error computing digest of downloaded s3 object %s/%s", spec.BucketName, spec.ObjectKey)
	}
	resolved, err := godigest.Parse(raw)
	if err != nil {
		return "", "", fmt.Errorf("downloaded s3 object %s/%s has an unparsable digest %q: %w", spec.BucketName, spec.ObjectKey, raw, err)
	}

	return resolved.Encoded(), result.VersionID, nil
}

// pinningVersion returns versionID unless it is the unversioned placeholder, which pins nothing.
func pinningVersion(versionID string) string {
	if versionID == download.UnversionedVersionID {
		return ""
	}
	return versionID
}

func (r *ResourceRepository) GetCredentialTypeScheme() *runtime.Scheme {
	return s3creds.Scheme
}
