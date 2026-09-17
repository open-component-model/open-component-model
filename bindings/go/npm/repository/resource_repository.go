// Package repository provides the npm resource repository: it downloads
// NPM/v1-accessed resources and computes their digests. Uploading to an npm
// registry is not supported.
package repository

import (
	"context"
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
	httpclient "ocm.software/open-component-model/bindings/go/http"
	"ocm.software/open-component-model/bindings/go/npm/internal/download"
	accessspec "ocm.software/open-component-model/bindings/go/npm/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/npm/spec/access/v1"
	credv1 "ocm.software/open-component-model/bindings/go/npm/spec/credentials/v1"
	identityv1 "ocm.software/open-component-model/bindings/go/npm/spec/identity/v1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// hashAlgorithmSHA256 is the hash algorithm used for npm resource digests.
	hashAlgorithmSHA256 = "SHA-256"
	// genericBlobDigestV1 is the normalisation algorithm for a plain downloaded blob.
	genericBlobDigestV1 = "genericBlobDigest/v1"
)

var (
	_ repository.ResourceRepository      = (*ResourceRepository)(nil)
	_ repository.ResourceDigestProcessor = (*ResourceRepository)(nil)
)

// ResourceRepository implements the ResourceRepository interface for npm access types.
type ResourceRepository struct {
	client           *http.Client
	maxDownloadSize  int64
	filesystemConfig *filesystemv1alpha1.Config
}

// NewResourceRepository creates a new npm resource repository. If filesystemConfig
// is non-nil, its TempFolder is used for the files tarballs are streamed into;
// otherwise os.CreateTemp's default directory is used.
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
		// The shared client brings retry and the configured TLS, CA and timeouts;
		// http.DefaultClient would bring none of them.
		client = httpclient.New(httpclient.WithConfig(options.HTTPConfig))
	}
	maxSize := DefaultMaxDownloadSize
	if options.MaxDownloadSize != nil {
		maxSize = *options.MaxDownloadSize
	}
	return &ResourceRepository{
		client:           client,
		maxDownloadSize:  maxSize,
		filesystemConfig: filesystemConfig,
	}
}

// GetResourceRepositoryScheme returns the scheme used by the npm resource repository.
func (r *ResourceRepository) GetResourceRepositoryScheme() *runtime.Scheme {
	return accessspec.Scheme
}

// GetResourceCredentialConsumerIdentity resolves the credential consumer identity for the given resource.
func (r *ResourceRepository) GetResourceCredentialConsumerIdentity(ctx context.Context, resource *descriptor.Resource) (runtime.Identity, error) {
	access, err := r.access(resource)
	if err != nil {
		return nil, err
	}

	identity, err := identityv1.IdentityFromRegistryAndPackage(access.Registry, access.Package)
	if err != nil {
		return nil, fmt.Errorf("error deriving npm identity: %w", err)
	}

	return identity, nil
}

// DownloadResource downloads the package tarball described by the npm access spec.
// The returned blob is backed by a file under the configured temp folder that outlives
// this call and is owned by the caller, and its content is verified against the
// checksums the registry published.
func (r *ResourceRepository) DownloadResource(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (blob.ReadOnlyBlob, error) {
	b, err := r.download(ctx, resource, credentials, r.tempFolder())
	if err != nil {
		return nil, err
	}
	return b, nil
}

// access converts the access spec of a resource into the typed npm access.
func (r *ResourceRepository) access(resource *descriptor.Resource) (*v1.NPM, error) {
	if resource == nil {
		return nil, fmt.Errorf("resource is required")
	}
	if resource.Access == nil {
		return nil, fmt.Errorf("resource access is required")
	}

	access := &v1.NPM{}
	if err := accessspec.Scheme.Convert(resource.Access, access); err != nil {
		return nil, fmt.Errorf("error converting resource access spec: %w", err)
	}
	if err := access.Validate(); err != nil {
		return nil, fmt.Errorf("invalid npm access spec: %w", err)
	}

	return access, nil
}

// tempFolder is the configured directory for temporary data, empty for the
// default of the operating system.
func (r *ResourceRepository) tempFolder() string {
	if r.filesystemConfig.TempFolder == nil {
		return ""
	}

	return *r.filesystemConfig.TempFolder
}

// download streams the package tarball into tempDir and returns it as a
// file-backed blob. The file outlives this call and is owned by the caller.
func (r *ResourceRepository) download(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed, tempDir string) (*filesystem.Blob, error) {
	access, err := r.access(resource)
	if err != nil {
		return nil, err
	}

	var creds *credv1.NPMCredentials
	if credentials != nil {
		if creds, err = credv1.ConvertToNPMCredentials(credentials); err != nil {
			return nil, err
		}
	}

	return download.Download(ctx, access, creds, download.Options{
		Client:          r.client,
		MaxDownloadSize: r.maxDownloadSize,
		TempDir:         tempDir,
	})
}

// UploadResource is not supported for npm access types.
func (r *ResourceRepository) UploadResource(ctx context.Context, res *descriptor.Resource, content blob.ReadOnlyBlob, credentials runtime.Typed) (*descriptor.Resource, error) {
	return nil, fmt.Errorf("upload is not supported for npm access type")
}

// GetResourceDigestProcessorCredentialConsumerIdentity resolves the credential consumer
// identity used when downloading the resource to compute its digest. It is the same identity
// used for a regular download, so credentials configured for the registry apply to both.
func (r *ResourceRepository) GetResourceDigestProcessorCredentialConsumerIdentity(ctx context.Context, resource *descriptor.Resource) (runtime.Identity, error) {
	return r.GetResourceCredentialConsumerIdentity(ctx, resource)
}

// ProcessResourceDigest computes the digest of an npm resource by downloading the
// referenced tarball and hashing it. When the resource already carries a digest, the
// computed value is verified against it. The tarball is only read here, so it is
// downloaded into a directory of its own that this call removes again.
func (r *ResourceRepository) ProcessResourceDigest(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (*descriptor.Resource, error) {
	tempDir, err := os.MkdirTemp(r.tempFolder(), "ocm-npm-digest-*")
	if err != nil {
		return nil, fmt.Errorf("error creating temporary directory for digest processing: %w", err)
	}
	defer func() {
		if rmErr := os.RemoveAll(tempDir); rmErr != nil {
			slog.WarnContext(ctx, "failed to remove temporary directory after digest processing", "path", tempDir, "err", rmErr)
		}
	}()

	data, err := r.download(ctx, resource, credentials, tempDir)
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

	resolvedValue := dig.Encoded()

	resource = resource.DeepCopy()
	if resource.Digest == nil {
		resource.Digest = &descriptor.Digest{
			HashAlgorithm:          hashAlgorithmSHA256,
			NormalisationAlgorithm: genericBlobDigestV1,
			Value:                  resolvedValue,
		}
		return resource, nil
	}

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

	return resource, nil
}
