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
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
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
	hashAlgorithmSHA512 = "SHA-512"
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

// ProcessResourceDigest computes the SHA-256 or SHA-512 digest of an S3 resource, which
// is the source of truth rather than the S3 ETag. When the resource already carries a
// digest, the computed value is verified against it, in that digest's algorithm.
//
// A store that keeps a SHA-256 or SHA-512 checksum of the whole object answers from a
// HeadObject, so the object is not transferred; see [ResourceRepository.resolveDigest].
//
// After a successful digest, the access is pinned to the object version that was read;
// see [ResourceRepository.pinAccess].
func (r *ResourceRepository) ProcessResourceDigest(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (*descriptor.Resource, error) {
	spec, err := r.convertAccess(resource)
	if err != nil {
		return nil, err
	}

	// A hand-written digest cannot know the normalisation algorithm and need not
	// restate the hash, so only a field pinned to an algorithm this processor does not
	// produce is a conflict. Spelling is not one either, hence the case-insensitive
	// comparisons.
	var want godigest.Algorithm
	if resource.Digest != nil {
		if want, err = requestedAlgorithm(resource.Digest.HashAlgorithm); err != nil {
			return nil, err
		}
		if resource.Digest.NormalisationAlgorithm != "" && !strings.EqualFold(resource.Digest.NormalisationAlgorithm, genericBlobDigestV1) {
			return nil, fmt.Errorf("normalisation algorithm mismatch: expected %s, got %s", genericBlobDigestV1, resource.Digest.NormalisationAlgorithm)
		}
	}

	resolved, versionID, err := r.resolveDigest(ctx, spec, credentials, want)
	if err != nil {
		return nil, err
	}

	resource = resource.DeepCopy()
	if resource.Digest != nil {
		value := resource.Digest.Value
		// The value may carry its algorithm as go-digest writes it, "sha512:<hex>", which
		// must then agree with the algorithm the digest names.
		if prefix, encoded, prefixed := strings.Cut(value, ":"); prefixed {
			if godigest.Algorithm(strings.ToLower(prefix)) != resolved.Algorithm() {
				return nil, fmt.Errorf("digest value %s carries algorithm %s, but the digest names %s", resource.Digest.Value, prefix, resolved.Algorithm())
			}
			value = encoded
		}
		if !strings.EqualFold(value, resolved.Encoded()) {
			return nil, fmt.Errorf("digest value mismatch: expected %s, got %s", resource.Digest.Value, resolved.Encoded())
		}
	}
	// Canonicalize the accepted spellings so descriptors do not vary by author.
	resource.Digest = &descriptor.Digest{
		HashAlgorithm:          ocmHashAlgorithms[resolved.Algorithm()],
		NormalisationAlgorithm: genericBlobDigestV1,
		Value:                  resolved.Encoded(),
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

// ocmHashAlgorithms are the digest algorithms this processor produces, by OCM name.
var ocmHashAlgorithms = map[godigest.Algorithm]string{
	godigest.SHA256: hashAlgorithmSHA256,
	godigest.SHA512: hashAlgorithmSHA512,
}

// requestedAlgorithm maps the hash algorithm of a digest already on the resource to
// the one to compute. A digest naming none was written against SHA-256, the default.
func requestedAlgorithm(name string) (godigest.Algorithm, error) {
	switch strings.ToLower(strings.ReplaceAll(name, "-", "")) {
	case "", "sha256":
		return godigest.SHA256, nil
	case "sha512":
		return godigest.SHA512, nil
	default:
		return "", fmt.Errorf("hash algorithm mismatch: expected %s or %s, got %s", ocmHashAlgorithms[godigest.SHA256], ocmHashAlgorithms[godigest.SHA512], name)
	}
}

// resolveDigest returns the digest of the object in algorithm want and the version it
// was taken at. An empty want takes SHA-256, or SHA-512 if the store keeps only that.
//
// It first asks the store with a HeadObject: a SHA-256 or SHA-512 checksum covering the
// whole object, which S3 verifies on upload and keeps for single-part uploads made with
// it, is the digest without transferring a byte. Everything else — no checksum, another
// algorithm, a multipart COMPOSITE checksum, a store not answering HEAD — falls back to
// downloading and hashing, in SHA-256 unless want says otherwise. Taking the store's
// word costs no integrity: every later download is verified against the digest this
// produces.
func (r *ResourceRepository) resolveDigest(ctx context.Context, spec *v2.S3, credentials runtime.Typed, want godigest.Algorithm) (godigest.Digest, string, error) {
	info, err := download.Head(ctx, request(spec), r.clientOptions(credentials)...)
	if err != nil {
		slog.DebugContext(ctx, "s3 HeadObject failed, digesting the object by download",
			slog.String("bucket", spec.BucketName), slog.String("objectKey", spec.ObjectKey), slog.String("err", err.Error()))
	} else {
		advertised := map[godigest.Algorithm]string{godigest.SHA256: info.SHA256, godigest.SHA512: info.SHA512}
		candidates := []godigest.Algorithm{godigest.SHA256, godigest.SHA512}
		if want != "" {
			candidates = []godigest.Algorithm{want}
		}
		for _, algorithm := range candidates {
			if value := advertised[algorithm]; value != "" {
				slog.DebugContext(ctx, "s3 object digest taken from the store's checksum",
					slog.String("bucket", spec.BucketName), slog.String("objectKey", spec.ObjectKey), slog.String("algorithm", algorithm.String()))
				return godigest.NewDigestFromEncoded(algorithm, value), info.VersionID, nil
			}
		}
	}

	if want == "" {
		want = godigest.SHA256
	}
	return r.digestByDownload(ctx, spec, credentials, want)
}

// digestByDownload downloads the object into a temporary directory it removes again
// and returns its digest in algorithm, with the version that was read.
func (r *ResourceRepository) digestByDownload(ctx context.Context, spec *v2.S3, credentials runtime.Typed, algorithm godigest.Algorithm) (godigest.Digest, string, error) {
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

	resolved, err := blobDigest(result.Blob, algorithm)
	if err != nil {
		return "", "", fmt.Errorf("error computing digest of downloaded s3 object %s/%s: %w", spec.BucketName, spec.ObjectKey, err)
	}

	return resolved, result.VersionID, nil
}

// blobDigest streams the file behind b through algorithm. It does not use the blob's own
// Digest, which holds the whole content in memory while hashing.
func blobDigest(b *filesystem.Blob, algorithm godigest.Algorithm) (godigest.Digest, error) {
	rc, err := b.ReadCloser()
	if err != nil {
		return "", err
	}
	defer func() { _ = rc.Close() }()

	return algorithm.FromReader(rc)
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
