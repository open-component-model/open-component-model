package repository

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"reflect"
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
	credsv1 "ocm.software/open-component-model/bindings/go/git/spec/credentials/v1"
	identityv1 "ocm.software/open-component-model/bindings/go/git/spec/identity/v1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	hashAlgorithmSHA256 = "SHA-256"
	genericBlobDigestV1 = "genericBlobDigest/v1"
)

type ResourceRepository struct {
	maxArchiveSize *int64

	hostKeyCallback  ssh.HostKeyCallback
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

	download.InstallHTTPClient(options.HTTPConfig)

	return &ResourceRepository{
		maxArchiveSize: options.MaxArchiveSize,

		hostKeyCallback:  options.HostKeyCallback,
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

// DownloadResource returns the archive of the resolved commit. It is backed by a
// file under the configured TempFolder, which outlives this call and is owned by
// the caller.
func (r *ResourceRepository) DownloadResource(ctx context.Context, res *descriptor.Resource, creds runtime.Typed) (blob.ReadOnlyBlob, error) {
	spec, err := accessFrom(res)
	if err != nil {
		return nil, err
	}

	result, err := r.download(ctx, spec, res.Digest, creds, r.tempFolder())
	if err != nil {
		return nil, err
	}

	return result.Blob, nil
}

func (r *ResourceRepository) download(ctx context.Context, spec *accessv1.Git, expected *descriptor.Digest, creds runtime.Typed, tempDir string) (*download.Result, error) {
	var typed *credsv1.GitCredentials
	if creds != nil {
		var err error
		if typed, err = credsv1.ConvertToGitCredentials(creds); err != nil {
			return nil, err
		}
	}

	result, err := download.Download(ctx, spec, typed, r.downloadOptions(tempDir))
	if err != nil {
		return nil, err
	}

	if err := verifyDigest(expected, result.Digest); err != nil {
		return nil, err
	}

	return result, nil
}

func (r *ResourceRepository) UploadResource(context.Context, *descriptor.Resource, blob.ReadOnlyBlob, runtime.Typed) (*descriptor.Resource, error) {
	return nil, fmt.Errorf("git repositories do not support upload operations")
}

func (r *ResourceRepository) GetResourceDigestProcessorCredentialConsumerIdentity(ctx context.Context, res *descriptor.Resource) (runtime.Identity, error) {
	return r.GetResourceCredentialConsumerIdentity(ctx, res)
}

// ProcessResourceDigest pins the access and hashes the same snapshot in one download.
// The archive is only read here, so it is downloaded into a directory of its own
// that this call removes again.
func (r *ResourceRepository) ProcessResourceDigest(ctx context.Context, res *descriptor.Resource, creds runtime.Typed) (*descriptor.Resource, error) {
	tempDir, err := os.MkdirTemp(r.tempFolder(), "ocm-git-digest-*")
	if err != nil {
		return nil, fmt.Errorf("cannot create temporary directory for digest processing: %w", err)
	}
	defer func() {
		if rmErr := os.RemoveAll(tempDir); rmErr != nil {
			slog.WarnContext(ctx, "failed to remove temporary directory after digest processing", "path", tempDir, "err", rmErr)
		}
	}()

	spec, err := accessFrom(res)
	if err != nil {
		return nil, err
	}

	downloaded, err := r.download(ctx, spec, res.Digest, creds, tempDir)
	if err != nil {
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
	// r.download already rejected a set digest that does not match this archive.
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
	if spec == nil || (reflect.ValueOf(spec).Kind() == reflect.Pointer && reflect.ValueOf(spec).IsNil()) {
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

	return &result, nil
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
