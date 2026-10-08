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
	"ocm.software/open-component-model/bindings/go/git/internal/endpoint"
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
	defer func() {
		if err := stream.Close(); err != nil {
			slog.WarnContext(ctx, "failed to close git archive", "err", err)
		}
	}()

	digester := digest.Canonical.Digester()
	tee := io.TeeReader(stream, digester.Hash())
	repo, commit, err := download.Import(ctx, tee, dir)
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

	if spec.Commit != "" && !strings.EqualFold(spec.Commit, commit.String()) {
		return nil, fmt.Errorf("git archive commit mismatch: expected %s, got %s", spec.Commit, commit)
	}
	if _, err := object.GetCommit(repo.Storer, commit); err != nil {
		return nil, fmt.Errorf("git archive HEAD %s is not a commit: %w", commit, err)
	}
	objects, err := download.HistoryObjects(repo, commit)
	if err != nil {
		return nil, err
	}
	history := make(map[plumbing.Hash]struct{}, len(objects))
	for _, hash := range objects {
		history[hash] = struct{}{}
	}
	if err := r.push(ctx, repo, spec, creds, commit, history); err != nil {
		return nil, err
	}
	return uploadedResource(resource, spec.Repository, spec.Ref, commit.String()), nil
}

// push points the target ref at commit. An existing ref must already name the
// commit (directly or as an annotated tag of it) or, for a branch, fast-forward
// to it within history, so the target never needs to be downloaded.
func (r *ResourceRepository) push(ctx context.Context, repo *git.Repository, target *accessv1.Git, creds *credsv1.GitCredentials, commit plumbing.Hash, history map[plumbing.Hash]struct{}) error {
	ep, err := endpoint.Parse(target.Repository)
	if err != nil {
		return fmt.Errorf("invalid target: cannot address git repository: %w", err)
	}
	options, err := download.ClientOptions(ep, creds, r.downloadOptions(r.tempFolder()))
	if err != nil {
		return fmt.Errorf("invalid target: %w", err)
	}
	remote, err := repo.CreateRemote(&config.RemoteConfig{Name: "ocm-target", URLs: []string{ep.URL}})
	if err != nil {
		return fmt.Errorf("cannot configure target repository: %w", err)
	}
	refs, err := remote.ListContext(ctx, &git.ListOptions{ClientOptions: options, PeelingOption: git.AppendPeeled})
	if err != nil && !errors.Is(err, transport.ErrEmptyRemoteRepository) {
		return download.TransportError(ctx, "cannot inspect target refs", err)
	}
	targetRef := plumbing.ReferenceName(target.Ref)
	advertised := make(map[plumbing.ReferenceName]plumbing.Hash, len(refs))
	for _, ref := range refs {
		advertised[ref.Name()] = ref.Hash()
	}
	if existing, ok := advertised[targetRef]; ok {
		if existing == commit || advertised[targetRef+"^{}"] == commit {
			return nil
		}
		if targetRef.IsTag() {
			return fmt.Errorf("target tag %q already exists at another commit", targetRef)
		}
		if _, ok := history[existing]; !ok {
			return fmt.Errorf("target ref %q does not fast-forward to %s", targetRef, commit)
		}
	}

	const uploadRef = "refs/ocm/upload"
	if err := repo.Storer.SetReference(plumbing.NewHashReference(uploadRef, commit)); err != nil {
		return fmt.Errorf("cannot stage git upload ref: %w", err)
	}
	if err := remote.PushContext(ctx, &git.PushOptions{
		RemoteName:    "ocm-target",
		RefSpecs:      []config.RefSpec{config.RefSpec(uploadRef + ":" + target.Ref)},
		ClientOptions: options,
	}); err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return download.TransportError(ctx, "cannot push git ref", err)
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
