package repository

import (
	"context"
	"crypto/fips140"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/git/internal/download"
	accessv1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
	credsv1 "ocm.software/open-component-model/bindings/go/git/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// UploadResource copies the Git history of an archive returned by DownloadResource
// to an existing repository and points one branch or tag at its commit. The
// resource access names the target repository and the full ref; a tag target
// becomes a lightweight tag. It never creates a commit or force-updates a ref.
func (r *ResourceRepository) UploadResource(ctx context.Context, resource *descriptor.Resource, content blob.ReadOnlyBlob, credentials runtime.Typed) (*descriptor.Resource, error) {
	spec, err := accessFrom(resource)
	if err != nil {
		return nil, err
	}
	ref := plumbing.ReferenceName(spec.Ref)
	if !ref.IsBranch() && !ref.IsTag() {
		return nil, fmt.Errorf("target ref must be a full branch or tag ref")
	}
	if content == nil {
		return nil, fmt.Errorf("git archive content is required")
	}
	creds, err := convertGitCredentials(credentials)
	if err != nil {
		return nil, fmt.Errorf("invalid target credentials: %w", err)
	}

	dir, err := os.MkdirTemp(r.tempFolder(), "ocm-git-upload-*")
	if err != nil {
		return nil, fmt.Errorf("cannot create temporary git storage: %w", err)
	}
	defer func() {
		if rmErr := os.RemoveAll(dir); rmErr != nil {
			slog.WarnContext(ctx, "failed to remove temporary git storage", "path", dir, "err", rmErr)
		}
	}()

	var uploaded *descriptor.Resource
	fips140.WithoutEnforcement(func() {
		uploaded, err = r.upload(ctx, resource, spec, content, creds, dir)
	})
	if err != nil {
		return nil, fmt.Errorf("cannot upload git resource: %w", err)
	}
	return uploaded, nil
}

func (r *ResourceRepository) upload(ctx context.Context, resource *descriptor.Resource, spec *accessv1.Git, content blob.ReadOnlyBlob, creds *credsv1.GitCredentials, dir string) (*descriptor.Resource, error) {
	stream, err := content.ReadCloser()
	if err != nil {
		return nil, fmt.Errorf("cannot read git archive: %w", err)
	}
	defer func() { _ = stream.Close() }()

	digester := digest.Canonical.Digester()
	tee := io.TeeReader(stream, digester.Hash())
	repo, commit, err := download.Import(tee, dir)
	if errors.Is(err, download.ErrNoHistory) {
		return nil, fmt.Errorf("git upload needs history: %w", err)
	}
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(io.Discard, tee); err != nil {
		return nil, fmt.Errorf("cannot read git archive: %w", err)
	}
	if err := verifyDigest(resource.Digest, digester.Digest()); err != nil {
		return nil, err
	}

	commits, _, err := download.VerifyObjectClosure(repo, commit)
	if err != nil {
		return nil, err
	}
	if _, ok := commits[commit]; !ok {
		return nil, fmt.Errorf("git archive HEAD %s is not a commit", commit)
	}
	if spec.Commit != "" && !strings.EqualFold(spec.Commit, commit.String()) {
		return nil, fmt.Errorf("git archive commit mismatch: expected %s, got %s", spec.Commit, commit)
	}
	return r.pushGitRepository(ctx, repo, resource, spec.Repository, plumbing.ReferenceName(spec.Ref), commit, commits, creds, r.downloadOptions(r.tempFolder()))
}

func (r *ResourceRepository) pushGitRepository(ctx context.Context, repo *git.Repository, source *descriptor.Resource, targetRepository string, targetRef plumbing.ReferenceName, commit plumbing.Hash, commits map[plumbing.Hash]struct{}, targetCreds *credsv1.GitCredentials, options download.Options) (*descriptor.Resource, error) {
	targetURL, targetOptions, err := download.RemoteOptions(targetRepository, targetCreds, options)
	if err != nil {
		return nil, fmt.Errorf("invalid target: %w", err)
	}
	remote, err := repo.CreateRemote(&config.RemoteConfig{Name: "ocm-target", URLs: []string{targetURL}})
	if err != nil {
		return nil, fmt.Errorf("cannot configure target repository: %w", err)
	}
	refs, err := remote.ListContext(ctx, &git.ListOptions{ClientOptions: targetOptions})
	if err != nil && !errors.Is(err, transport.ErrEmptyRemoteRepository) {
		return nil, download.TransportError(ctx, "cannot inspect target refs", err)
	}
	for _, existing := range refs {
		if existing.Name() != targetRef {
			continue
		}
		if existing.Hash() != commit {
			if strings.HasPrefix(string(targetRef), "refs/tags/") {
				return nil, fmt.Errorf("target tag %q already exists at another object", targetRef)
			}
			if _, ok := commits[existing.Hash()]; !ok {
				return nil, fmt.Errorf("target ref %q does not fast-forward to %s", targetRef, commit)
			}
		}
		if err := r.verifyTargetHistory(ctx, targetRepository, string(targetRef), existing.Hash(), targetCreds); err != nil {
			return nil, err
		}
		if existing.Hash() == commit {
			return uploadedResource(source, targetRepository, string(targetRef), commit.String()), nil
		}
		break
	}
	const uploadRef = "refs/ocm/upload"
	if err := repo.Storer.SetReference(plumbing.NewHashReference(uploadRef, commit)); err != nil {
		return nil, fmt.Errorf("cannot stage git upload ref: %w", err)
	}
	if err := remote.PushContext(ctx, &git.PushOptions{
		RemoteName:    "ocm-target",
		RefSpecs:      []config.RefSpec{config.RefSpec(uploadRef + ":" + string(targetRef))},
		ClientOptions: targetOptions,
	}); err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return nil, download.TransportError(ctx, "cannot push git ref", err)
	}
	return uploadedResource(source, targetRepository, string(targetRef), commit.String()), nil
}

func (r *ResourceRepository) verifyTargetHistory(ctx context.Context, repository, ref string, tip plumbing.Hash, credentials *credsv1.GitCredentials) error {
	opts := r.downloadOptions(r.tempFolder())
	access := &accessv1.Git{Repository: repository, Ref: ref, Commit: tip.String()}
	err := download.WithRepository(ctx, access, credentials, opts, func(target *git.Repository, _ *object.Commit) error {
		_, _, err := download.VerifyObjectClosure(target, tip)
		return err
	})
	if err != nil {
		return fmt.Errorf("target repository has incomplete history at %s: %w", tip, err)
	}
	return nil
}

func convertGitCredentials(typed runtime.Typed) (*credsv1.GitCredentials, error) {
	if typed == nil {
		return nil, nil
	}
	return credsv1.ConvertToGitCredentials(typed)
}

func uploadedResource(source *descriptor.Resource, repository, ref, commit string) *descriptor.Resource {
	result := source.DeepCopy()
	result.Access = &accessv1.Git{
		Type:       runtime.NewVersionedType(accessv1.Type, accessv1.Version),
		Repository: repository,
		Ref:        ref,
		Commit:     commit,
	}
	return result
}
