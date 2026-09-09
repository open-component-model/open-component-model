package download

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"ocm.software/open-component-model/bindings/go/git/internal/endpoint"
	accessv1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
	credsv1 "ocm.software/open-component-model/bindings/go/git/spec/credentials/v1"
)

// Download resolves one snapshot and returns its archive and full commit SHA.
func Download(ctx context.Context, access *accessv1.Git, creds *credsv1.GitCredentials, opts Options) (_ *Blob, commit string, err error) {
	if err := access.Validate(); err != nil {
		return nil, "", err
	}

	if err := ctx.Err(); err != nil {
		return nil, "", err
	}

	ep, err := endpoint.Parse(access.Repository)
	if err != nil {
		return nil, "", err
	}

	auth, err := authMethod(ep, creds, opts)
	if err != nil {
		return nil, "", err
	}

	dir, err := os.MkdirTemp(opts.TempDir, "ocm-git-repository-*")
	if err != nil {
		return nil, "", fmt.Errorf("cannot create git storage: %w", err)
	}

	var result *Blob
	defer func() {
		err = errors.Join(err, os.RemoveAll(dir))
		if err != nil && result != nil {
			err = errors.Join(err, result.Close())
		}
	}()

	var repo *git.Repository
	if access.Commit == "" {
		repo, err = git.PlainCloneContext(ctx, dir, false, &git.CloneOptions{
			URL:        access.Repository,
			Auth:       auth,
			NoCheckout: true,
			Tags:       git.AllTags,
			CABundle:   opts.CABundle,
		})
	} else {
		// A pinned commit must not depend on a valid remote HEAD.
		repo, err = git.PlainInit(dir, false)
		if err == nil {
			_, err = repo.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{access.Repository}})
		}

		if err == nil {
			err = repo.FetchContext(ctx, &git.FetchOptions{
				Auth:     auth,
				CABundle: opts.CABundle,
				Tags:     git.AllTags,
				RefSpecs: []config.RefSpec{"+refs/*:refs/*"},
			})

			if errors.Is(err, git.NoErrAlreadyUpToDate) {
				err = nil
			}
		}
	}

	if repo != nil {
		if closer, ok := repo.Storer.(io.Closer); ok {
			defer func() { err = errors.Join(err, closer.Close()) }()
		}
	}

	if err != nil {
		return nil, "", transportError(ctx, "cannot fetch git repository", err)
	}

	var hash *plumbing.Hash
	if access.Commit != "" {
		value := plumbing.NewHash(access.Commit)
		hash = &value
		if _, objectErr := repo.CommitObject(value); objectErr != nil {
			// Some servers expose a pinned commit only through an explicit object fetch.
			err = repo.FetchContext(ctx, &git.FetchOptions{
				Auth:     auth,
				CABundle: opts.CABundle,
				Tags:     git.NoTags,
				RefSpecs: []config.RefSpec{config.RefSpec("+" + value.String() + ":refs/ocm/commit")},
			})

			if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
				return nil, "", transportError(ctx, "cannot fetch pinned git commit", err)
			}
		}
	} else {
		if access.Ref != "HEAD" && (strings.HasPrefix(access.Ref, "refs/heads/") || !strings.HasPrefix(access.Ref, "refs/")) {
			hash, err = repo.ResolveRevision(plumbing.Revision("refs/remotes/origin/" + strings.TrimPrefix(access.Ref, "refs/heads/")))
		}

		if hash == nil || err != nil {
			hash, err = repo.ResolveRevision(plumbing.Revision(access.Ref))
		}

		if err != nil {
			return nil, "", fmt.Errorf("cannot resolve git ref: %w", err)
		}
	}

	selected, err := peelCommit(repo, *hash)
	if err != nil {
		return nil, "", err
	}

	if err := ctx.Err(); err != nil {
		return nil, "", err
	}

	worktree, err := repo.Worktree()
	if err != nil {
		return nil, "", fmt.Errorf("cannot open git worktree: %w", err)
	}

	if err := worktree.Checkout(&git.CheckoutOptions{Hash: selected.Hash}); err != nil {
		return nil, "", fmt.Errorf("cannot check out git commit: %w", err)
	}

	result, err = archive(ctx, dir, opts)
	if err != nil {
		return nil, "", err
	}

	return result, selected.Hash.String(), nil
}

func peelCommit(repo *git.Repository, hash plumbing.Hash) (*object.Commit, error) {
	for range 32 {
		obj, err := repo.Object(plumbing.AnyObject, hash)
		if err != nil {
			return nil, fmt.Errorf("cannot read git revision: %w", err)
		}

		switch obj := obj.(type) {
		case *object.Commit:
			return obj, nil
		case *object.Tag:
			hash = obj.Target
		default:
			return nil, fmt.Errorf("git revision does not identify a commit")
		}
	}

	return nil, fmt.Errorf("git tag nesting exceeds the limit")
}

// Transport errors can contain credentials from the remote URL or server body.
func transportError(ctx context.Context, operation string, err error) error {
	if ctx.Err() != nil {
		return fmt.Errorf("%s: %w", operation, ctx.Err())
	}

	switch {
	case errors.Is(err, git.ErrRepositoryNotExists):
		return fmt.Errorf("%s: repository not found", operation)
	case strings.Contains(err.Error(), "authentication required"):
		return fmt.Errorf("%s: authentication required", operation)
	default:
		return fmt.Errorf("%s: transport failed; check repository access and server trust", operation)
	}
}
