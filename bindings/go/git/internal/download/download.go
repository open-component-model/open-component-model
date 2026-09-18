package download

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"regexp"
	"strings"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/git/internal/endpoint"
	accessv1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
	credsv1 "ocm.software/open-component-model/bindings/go/git/spec/credentials/v1"
)

// Result is one downloaded snapshot of a Git repository, archived as tar.
type Result struct {
	// Blob is backed by a file that outlives the call and is owned by the caller.
	Blob *filesystem.Blob
	// Commit is the full SHA the archive was taken from.
	Commit string
	// Digest is taken while the archive is written.
	Digest digest.Digest
}

// Download resolves one snapshot of the repository and archives it.
func Download(ctx context.Context, access *accessv1.Git, creds *credsv1.GitCredentials, opts Options) (_ *Result, err error) {
	if err := access.Validate(); err != nil {
		return nil, fmt.Errorf("invalid git access: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("cannot download git repository: %w", err)
	}

	ep, err := endpoint.Parse(access.Repository)
	if err != nil {
		return nil, fmt.Errorf("cannot address git repository: %w", err)
	}

	auth, err := authMethod(ep, creds, opts)
	if err != nil {
		return nil, fmt.Errorf("cannot authenticate against git repository: %w", err)
	}

	dir, err := os.MkdirTemp(opts.TempDir, "ocm-git-repository-*")
	if err != nil {
		return nil, fmt.Errorf("cannot create git storage: %w", err)
	}

	// The git storage always goes, and failing to remove it does not invalidate the
	// download, so it is logged rather than returned. The archive file is removed
	// only when this call fails; on success it belongs to the caller.
	var archivePath string
	defer func() {
		if rmErr := os.RemoveAll(dir); rmErr != nil {
			slog.WarnContext(ctx, "failed to remove temporary git storage", "path", dir, "err", rmErr)
		}

		if err != nil && archivePath != "" {
			if rmErr := removeIgnoringMissing(archivePath); rmErr != nil {
				slog.WarnContext(ctx, "failed to remove incomplete git archive", "path", archivePath, "err", rmErr)
			}
		}
	}()

	var repo *git.Repository
	if access.Commit == "" {
		repo, err = git.PlainCloneContext(ctx, dir, true, &git.CloneOptions{
			URL:      access.Repository,
			Auth:     auth,
			Tags:     git.AllTags,
			CABundle: opts.CABundle,
		})
		if err != nil {
			err = transportError(ctx, "cannot fetch git repository", err)
		}
	} else {
		repo, err = fetchCommit(ctx, dir, access, auth, opts)
	}

	if repo != nil {
		if closer, ok := repo.Storer.(io.Closer); ok {
			defer func() { err = errors.Join(err, closer.Close()) }()
		}
	}

	if err != nil {
		return nil, err
	}

	var hash *plumbing.Hash
	if access.Commit != "" {
		value := plumbing.NewHash(access.Commit)
		hash = &value
	} else {
		if access.Ref != "HEAD" && (strings.HasPrefix(access.Ref, "refs/heads/") || !strings.HasPrefix(access.Ref, "refs/")) {
			hash, err = repo.ResolveRevision(plumbing.Revision("refs/remotes/origin/" + strings.TrimPrefix(access.Ref, "refs/heads/")))
		}

		if hash == nil || err != nil {
			hash, err = repo.ResolveRevision(plumbing.Revision(access.Ref))
		}

		if err != nil {
			return nil, fmt.Errorf("cannot resolve git ref: %w", err)
		}
	}

	selected, err := peelCommit(repo, *hash)
	if err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("cannot archive git repository: %w", err)
	}

	file, err := os.CreateTemp(opts.TempDir, "ocm-git-archive-*.tar")
	if err != nil {
		return nil, fmt.Errorf("cannot create git archive file: %w", err)
	}
	archivePath = file.Name()

	b, archiveDigest, err := archive(ctx, selected, file, opts)
	if err != nil {
		return nil, err
	}

	return &Result{Blob: b, Commit: selected.Hash.String(), Digest: archiveDigest}, nil
}

// fetchCommit fetches a pinned commit without depending on a valid remote HEAD.
// The repository is returned also with a fetch error, so the caller can close its storage.
func fetchCommit(ctx context.Context, dir string, access *accessv1.Git, auth transport.AuthMethod, opts Options) (*git.Repository, error) {
	repo, err := git.PlainInit(dir, true)
	if err != nil {
		return nil, fmt.Errorf("cannot create git repository: %w", err)
	}

	if _, err := repo.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{access.Repository}}); err != nil {
		return repo, fmt.Errorf("cannot configure git remote: %w", err)
	}

	// Asking for the pinned commit alone avoids transferring every ref and its
	// history. Servers that do not advertise allow-tip-sha1-in-want or
	// allow-reachable-sha1-in-want reject the request before any transfer; the
	// fetch of all refs below is the fallback.
	err = repo.FetchContext(ctx, &git.FetchOptions{
		Auth:     auth,
		CABundle: opts.CABundle,
		Tags:     git.NoTags,
		RefSpecs: []config.RefSpec{config.RefSpec("+" + access.Commit + ":refs/ocm/commit")},
	})
	if errors.Is(err, git.ErrExactSHA1NotSupported) {
		err = repo.FetchContext(ctx, &git.FetchOptions{
			Auth:     auth,
			CABundle: opts.CABundle,
			Tags:     git.AllTags,
			RefSpecs: []config.RefSpec{"+refs/*:refs/*"},
		})
	}

	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return repo, transportError(ctx, "cannot fetch git repository", err)
	}

	return repo, nil
}

// peelCommit follows an annotated tag to the commit it points at. A tag can
// target another tag, so the loop repeats; the bound stops a tag cycle.
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

// removeIgnoringMissing deletes path, treating an already deleted file as success.
func removeIgnoringMissing(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("cannot remove %q: %w", path, err)
	}

	return nil
}

// transportError names the common transport failures and keeps a redacted cause
// for the ones it cannot name.
func transportError(ctx context.Context, operation string, err error) error {
	if ctx.Err() != nil {
		return fmt.Errorf("%s: %w", operation, ctx.Err())
	}

	switch {
	case errors.Is(err, git.ErrRepositoryNotExists):
		return mask(err, "%s: repository not found", operation)
	case errors.Is(err, transport.ErrAuthenticationRequired):
		return mask(err, "%s: authentication required", operation)
	case errors.Is(err, transport.ErrAuthorizationFailed):
		return mask(err, "%s: authorization failed", operation)
	default:
		return mask(err, "%s: transport failed; check repository access and server trust: %s", operation, redact(err))
	}
}

// mask reports err under a message of our own, because a transport error quotes
// the remote URL with its credentials. errors.Is and errors.As still reach err.
func mask(err error, format string, args ...any) error {
	return &maskedError{err: err, message: fmt.Sprintf(format, args...)}
}

type maskedError struct {
	err     error
	message string
}

func (e *maskedError) Error() string { return e.message }

func (e *maskedError) Unwrap() error { return e.err }

// userinfo matches the credentials a quoted remote URL carries into an error.
var userinfo = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.\-]*://)[^/@\s]+@`)

// maxCauseLength bounds the cause: go-git puts the whole response body of an
// unrecognised status into its error.
const maxCauseLength = 512

// redact removes the credentials of every URL the message quotes.
func redact(err error) string {
	message := userinfo.ReplaceAllString(err.Error(), "${1}xxxxx@")
	if len(message) > maxCauseLength {
		message = message[:maxCauseLength] + "..."
	}

	return message
}
