package repository

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/opencontainers/go-digest"
	"golang.org/x/crypto/ssh"

	"ocm.software/open-component-model/bindings/go/blob"
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/git/internal/download"
	"ocm.software/open-component-model/bindings/go/git/spec/access"
	accessv1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
	gitcreds "ocm.software/open-component-model/bindings/go/git/spec/credentials"
	identityv1 "ocm.software/open-component-model/bindings/go/git/spec/identity/v1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/repository/integrity"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	hashAlgorithmSHA256 = "SHA-256"
	genericBlobDigestV1 = "genericBlobDigest/v1"
)

type ResourceRepository struct {
	maxArchiveSize *int64

	hostKeyCallback  ssh.HostKeyCallback
	httpClient       *http.Client
	filesystemConfig *filesystemv1alpha1.Config
}

var (
	_ repository.ResourceRepository      = (*ResourceRepository)(nil)
	_ repository.ResourceDigestProcessor = (*ResourceRepository)(nil)
)

// NewResourceRepository creates a new Git resource repository. If filesystemConfig
// is non-nil, its TempFolder holds the Git objects and archives a download creates;
// otherwise the default directory of os.MkdirTemp is used.
func NewResourceRepository(filesystemConfig *filesystemv1alpha1.Config, opts ...Option) *ResourceRepository {
	if filesystemConfig == nil {
		filesystemConfig = &filesystemv1alpha1.Config{}
	}
	options := &Options{}
	for _, opt := range opts {
		opt(options)
	}

	return &ResourceRepository{
		maxArchiveSize: options.MaxArchiveSize,

		hostKeyCallback:  options.HostKeyCallback,
		httpClient:       options.HTTPClient,
		filesystemConfig: filesystemConfig,
	}
}

// tempFolder is the configured directory for temporary data, empty for the
// default of the operating system.
func (r *ResourceRepository) tempFolder() string {
	if r.filesystemConfig.TempFolder == nil {
		return ""
	}

	return *r.filesystemConfig.TempFolder
}

// downloadOptions resolves the per-download configuration, applying the defaults
// for anything the caller left unset.
func (r *ResourceRepository) downloadOptions(tempDir string) download.Options {
	maxArchiveSize := download.DefaultMaxArchiveSize
	if r.maxArchiveSize != nil {
		maxArchiveSize = *r.maxArchiveSize
	}

	return download.Options{
		TempDir:        tempDir,
		MaxArchiveSize: maxArchiveSize,

		HostKeyCallback: r.hostKeyCallback,
		HTTPClient:      r.httpClient,
	}
}

func (r *ResourceRepository) GetCredentialTypeScheme() *runtime.Scheme {
	return gitcreds.Scheme
}

func (r *ResourceRepository) GetResourceRepositoryScheme() *runtime.Scheme {
	return access.Scheme
}

func (r *ResourceRepository) GetResourceCredentialConsumerIdentity(_ context.Context, res *descriptor.Resource) (runtime.Identity, error) {
	spec, err := accessFrom(res)
	if err != nil {
		return nil, err
	}

	return identityv1.IdentityFromURL(spec.Repository)
}

// DownloadResource returns the archive of the resolved commit as a lazy blob (see
// [download.Blob]): nothing is stored and nothing is left to clean up. Reads are
// verified against res.Digest and bound to ctx (see package repository). Errors of
// the fetch surface on read.
func (r *ResourceRepository) DownloadResource(ctx context.Context, res *descriptor.Resource, creds runtime.Typed) (blob.ReadOnlyBlob, error) {
	spec, err := accessFrom(res)
	if err != nil {
		return nil, err
	}
	typed, err := convertGitCredentials(creds)
	if err != nil {
		return nil, err
	}

	archive := download.NewBlob(ctx, spec, typed, r.downloadOptions(r.tempFolder()))
	b, err := integrity.Verify(ctx, res.Digest, archive, integrity.WithLogAttrs(slog.Any("resource", res.ToIdentity())))
	if err != nil {
		return nil, fmt.Errorf("resource %q: %w", res.Name, err)
	}
	return b, nil
}

func (r *ResourceRepository) GetResourceDigestProcessorCredentialConsumerIdentity(ctx context.Context, res *descriptor.Resource) (runtime.Identity, error) {
	return r.GetResourceCredentialConsumerIdentity(ctx, res)
}

// ProcessResourceDigest pins the access and hashes the same snapshot in one download.
// Pinning records the commit (see [download.WithRepository]).
// The archive is only hashed here, so it is never stored (see [download.Stream]).
func (r *ResourceRepository) ProcessResourceDigest(ctx context.Context, res *descriptor.Resource, creds runtime.Typed) (*descriptor.Resource, error) {
	spec, err := accessFrom(res)
	if err != nil {
		return nil, err
	}
	typed, err := convertGitCredentials(creds)
	if err != nil {
		return nil, err
	}

	downloaded, err := download.Stream(ctx, spec, typed, io.Discard, r.downloadOptions(r.tempFolder()))
	if err != nil {
		return nil, err
	}
	if err := verifyDigest(res.Digest, downloaded.Digest); err != nil {
		return nil, err
	}

	// A set commit is authoritative, only a ref-only access gets pinned.
	if spec.Commit == "" {
		spec.Commit = downloaded.Commit
	}
	pinned := &runtime.Raw{}
	if err := access.Scheme.Convert(spec, pinned); err != nil {
		return nil, fmt.Errorf("cannot encode pinned git access: %w", err)
	}

	result := res.DeepCopy()
	result.Access = pinned
	// verifyDigest already rejected a set digest that does not match this archive.
	result.Digest = &descriptor.Digest{
		HashAlgorithm:          hashAlgorithmSHA256,
		NormalisationAlgorithm: genericBlobDigestV1,
		Value:                  downloaded.Digest.Encoded(),
	}

	return result, nil
}

func accessFrom(res *descriptor.Resource) (*accessv1.Git, error) {
	if res == nil {
		return nil, fmt.Errorf("resource is required")
	}

	spec := res.Access
	if spec == nil {
		return nil, fmt.Errorf("git access is required")
	}

	if _, err := access.Scheme.NewObject(spec.GetType()); err != nil {
		return nil, fmt.Errorf("unsupported git access type: %w", err)
	}

	var result accessv1.Git
	if err := access.Scheme.Convert(spec, &result); err != nil {
		return nil, fmt.Errorf("cannot decode git access: %w", err)
	}

	if err := result.Validate(); err != nil {
		return nil, err
	}

	return result.Normalize()
}

func verifyDigest(expected *descriptor.Digest, actual digest.Digest) error {
	if err := actual.Validate(); err != nil {
		return fmt.Errorf("git archive has an invalid digest %q: %w", actual, err)
	}

	if expected == nil {
		return nil
	}

	if expected.HashAlgorithm != "" && !strings.EqualFold(expected.HashAlgorithm, hashAlgorithmSHA256) {
		return fmt.Errorf("unsupported git hash algorithm %q", expected.HashAlgorithm)
	}

	if expected.NormalisationAlgorithm != "" && !strings.EqualFold(expected.NormalisationAlgorithm, genericBlobDigestV1) {
		return fmt.Errorf("unsupported git normalisation algorithm %q", expected.NormalisationAlgorithm)
	}

	if !strings.EqualFold(expected.Value, actual.Encoded()) {
		return fmt.Errorf("git archive digest mismatch: expected %s, got %s", expected.Value, actual.Encoded())
	}

	return nil
}
