package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/transport"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/git/internal/download"
	accessv1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
	credsv1 "ocm.software/open-component-model/bindings/go/git/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var errIncompleteObjects = errors.New("incomplete git object history")

// UploadOptions identifies the target ref and controls source fetch overrides.
type UploadOptions struct {
	Repository string
	Ref        string
	Depth      *int
	Filter     *string
	MaxDepth   int
}

// UploadGit copies original Git objects to an existing repository and advances one ref.
// It never creates a commit or force-updates a ref.
func (r *ResourceRepository) UploadGit(ctx context.Context, source *descriptor.Resource, target UploadOptions, sourceCredentials, targetCredentials runtime.Typed) (*descriptor.Resource, error) {
	spec, err := accessFrom(source)
	if err != nil {
		return nil, err
	}
	if spec.Commit == "" {
		return nil, fmt.Errorf("git upload requires a pinned commit")
	}
	ref := plumbing.ReferenceName(target.Ref)
	if !strings.HasPrefix(target.Ref, "refs/heads/") && !strings.HasPrefix(target.Ref, "refs/tags/") {
		return nil, fmt.Errorf("target ref must be a full branch or tag ref")
	}
	if err := ref.Validate(); err != nil {
		return nil, fmt.Errorf("invalid target ref: %w", err)
	}
	if target.Repository == "" {
		return nil, fmt.Errorf("target repository is required")
	}
	if target.MaxDepth < 0 {
		return nil, fmt.Errorf("maxDepth must not be negative")
	}
	sourceCreds, err := convertGitCredentials(sourceCredentials)
	if err != nil {
		return nil, fmt.Errorf("invalid source credentials: %w", err)
	}
	targetCreds, err := convertGitCredentials(targetCredentials)
	if err != nil {
		return nil, fmt.Errorf("invalid target credentials: %w", err)
	}
	fetchOptions := r.downloadOptions(r.tempFolder())
	fetchOptions.Depth = spec.Depth
	fetchOptions.Filter = spec.Filter
	if target.Depth != nil {
		fetchOptions.Depth = *target.Depth
	}
	if target.Filter != nil {
		fetchOptions.Filter = *target.Filter
	}
	if fetchOptions.Depth < 0 || (fetchOptions.Filter != "" && fetchOptions.Filter != "blob:none") {
		return nil, fmt.Errorf("invalid upload fetch options")
	}
	if target.MaxDepth > 0 {
		if fetchOptions.Depth > target.MaxDepth {
			return nil, fmt.Errorf("depth exceeds maxDepth")
		}
		if fetchOptions.Depth == 0 {
			fetchOptions.Depth = target.MaxDepth
		}
	}
	var uploaded *descriptor.Resource
	upload := func(repo *git.Repository, selected *object.Commit) error {
		result, err := r.uploadGitRepository(ctx, repo, selected, source, spec, target.Repository, ref, sourceCreds, targetCreds, fetchOptions)
		uploaded = result
		return err
	}
	err = download.WithRepository(ctx, spec, sourceCreds, fetchOptions, upload)
	if errors.Is(err, errIncompleteObjects) && (fetchOptions.Depth != target.MaxDepth || fetchOptions.Filter != "") {
		fetchOptions.Depth = target.MaxDepth
		fetchOptions.Filter = ""
		err = download.WithRepository(ctx, spec, sourceCreds, fetchOptions, upload)
	}
	if err != nil {
		return nil, fmt.Errorf("cannot upload git resource: %w", err)
	}
	return uploaded, nil
}

func (r *ResourceRepository) uploadGitRepository(ctx context.Context, repo *git.Repository, selected *object.Commit, source *descriptor.Resource, sourceSpec *accessv1.Git, targetRepository string, targetRef plumbing.ReferenceName, sourceCreds, targetCreds *credsv1.GitCredentials, options download.Options) (*descriptor.Resource, error) {
	pushHash := selected.Hash
	if strings.HasPrefix(string(targetRef), "refs/tags/") && strings.HasPrefix(sourceSpec.Ref, "refs/tags/") {
		var err error
		pushHash, err = fetchPinnedTag(ctx, repo, sourceSpec.Ref, selected.Hash, sourceSpec.Repository, options, sourceCreds)
		if err != nil {
			return nil, err
		}
	}
	commits, err := verifyObjectClosure(repo, pushHash)
	if err != nil {
		return nil, err
	}
	if source.Digest != nil {
		archiveDigest, err := download.ArchiveDigest(ctx, selected, options)
		if err != nil {
			return nil, fmt.Errorf("cannot verify git resource digest: %w", err)
		}
		if err := verifyDigest(source.Digest, archiveDigest); err != nil {
			return nil, err
		}
	}
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
		if err := r.verifyTargetHistory(ctx, targetRepository, existing.Hash(), targetCreds); err != nil {
			return nil, err
		}
		if existing.Hash() == pushHash {
			return uploadedResource(source, targetRepository, string(targetRef), selected.Hash.String()), nil
		}
		if strings.HasPrefix(string(targetRef), "refs/tags/") {
			return nil, fmt.Errorf("target tag %q already exists at another object", targetRef)
		}
		if _, ok := commits[existing.Hash()]; !ok {
			return nil, fmt.Errorf("target ref %q does not fast-forward to %s", targetRef, selected.Hash)
		}
		break
	}
	const uploadRef = "refs/ocm/upload"
	if err := repo.Storer.SetReference(plumbing.NewHashReference(uploadRef, pushHash)); err != nil {
		return nil, fmt.Errorf("cannot stage git upload ref: %w", err)
	}
	if err := remote.PushContext(ctx, &git.PushOptions{
		RemoteName:    "ocm-target",
		RefSpecs:      []config.RefSpec{config.RefSpec(uploadRef + ":" + string(targetRef))},
		ClientOptions: targetOptions,
	}); err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return nil, download.TransportError(ctx, "cannot push git ref", err)
	}
	return uploadedResource(source, targetRepository, string(targetRef), selected.Hash.String()), nil
}

func (r *ResourceRepository) verifyTargetHistory(ctx context.Context, repository string, tip plumbing.Hash, credentials *credsv1.GitCredentials) error {
	opts := r.downloadOptions(r.tempFolder())
	access := &accessv1.Git{Repository: repository, Commit: tip.String()}
	err := download.WithRepository(ctx, access, credentials, opts, func(target *git.Repository, _ *object.Commit) error {
		_, err := verifyObjectClosure(target, tip)
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

func verifyObjectClosure(repo *git.Repository, tip plumbing.Hash) (map[plumbing.Hash]struct{}, error) {
	seen := make(map[plumbing.Hash]struct{})
	commits := make(map[plumbing.Hash]struct{})
	pending := []plumbing.Hash{tip}
	for len(pending) > 0 {
		hash := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if _, ok := seen[hash]; ok {
			continue
		}
		seen[hash] = struct{}{}
		encoded, err := repo.Storer.EncodedObject(plumbing.AnyObject, hash)
		if err != nil {
			return nil, fmt.Errorf("%w: object %s: %w", errIncompleteObjects, hash, err)
		}
		switch encoded.Type() {
		case plumbing.CommitObject:
			commit, err := object.GetCommit(repo.Storer, hash)
			if err != nil {
				return nil, fmt.Errorf("cannot read commit %s: %w", hash, err)
			}
			commits[hash] = struct{}{}
			pending = append(pending, commit.TreeHash)
			pending = append(pending, commit.ParentHashes...)
		case plumbing.TreeObject:
			tree, err := object.GetTree(repo.Storer, hash)
			if err != nil {
				return nil, fmt.Errorf("cannot read tree %s: %w", hash, err)
			}
			for _, entry := range tree.Entries {
				if entry.Mode != filemode.Submodule {
					pending = append(pending, entry.Hash)
				}
			}
		case plumbing.TagObject:
			tag, err := object.GetTag(repo.Storer, hash)
			if err != nil {
				return nil, fmt.Errorf("cannot read tag %s: %w", hash, err)
			}
			pending = append(pending, tag.Target)
		case plumbing.BlobObject:
		default:
			return nil, fmt.Errorf("unsupported git object type %s", encoded.Type())
		}
	}
	return commits, nil
}

func fetchPinnedTag(ctx context.Context, repo *git.Repository, sourceRef string, commit plumbing.Hash, sourceRepository string, options download.Options, sourceCreds *credsv1.GitCredentials) (plumbing.Hash, error) {
	_, clientOptions, err := download.RemoteOptions(sourceRepository, sourceCreds, options)
	if err != nil {
		return plumbing.ZeroHash, err
	}
	const tagRef = "refs/ocm/source-tag"
	err = repo.FetchContext(ctx, &git.FetchOptions{
		RefSpecs:      []config.RefSpec{config.RefSpec("+" + sourceRef + ":" + tagRef)},
		ClientOptions: clientOptions,
		Tags:          git.NoTags,
		Depth:         options.Depth,
		Filter:        packp.Filter(options.Filter),
	})
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return plumbing.ZeroHash, download.TransportError(ctx, "cannot fetch source tag", err)
	}
	ref, err := repo.Reference(tagRef, true)
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("cannot resolve source tag: %w", err)
	}
	hash := ref.Hash()
	for range 32 {
		if hash == commit {
			return ref.Hash(), nil
		}
		tag, err := object.GetTag(repo.Storer, hash)
		if err != nil {
			return plumbing.ZeroHash, fmt.Errorf("source tag does not point to pinned commit: %w", err)
		}
		hash = tag.Target
	}
	return plumbing.ZeroHash, fmt.Errorf("source tag nesting exceeds the limit")
}
