package download

import (
	"context"
	"crypto/fips140"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"regexp"
	"strings"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/client"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/git/internal/endpoint"
	accessv1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
	credsv1 "ocm.software/open-component-model/bindings/go/git/spec/credentials/v1"
	ocmhttp "ocm.software/open-component-model/bindings/go/http"
)

// Result is one downloaded snapshot of a Git repository with its history, archived as tar.gz.
type Result struct {
	// Blob is backed by a file that outlives the call and is owned by the caller.
	Blob *filesystem.Blob
	// Commit is the full SHA the archive was taken from.
	Commit string
	// Ref is the full name of the ref the access selects (see WithRepository), or empty
	// for an access without ref.
	Ref string
	// Digest covers the final compressed archive bytes.
	Digest digest.Digest
}

// Download resolves one snapshot of the repository and archives it with its history.
func Download(ctx context.Context, access *accessv1.Git, creds *credsv1.GitCredentials, opts Options) (*Result, error) {
	var (
		result      *Result
		archivePath string
	)
	err := WithRepository(ctx, access, creds, opts, func(repo *git.Repository, selected *object.Commit, ref plumbing.ReferenceName) error {
		file, err := os.CreateTemp(opts.TempDir, "ocm-git-archive-*.tar.gz")
		if err != nil {
			return fmt.Errorf("cannot create git archive file: %w", err)
		}
		archivePath = file.Name()
		b, archiveDigest, err := archive(ctx, repo, selected, file, opts)
		if err != nil {
			return err
		}
		result = &Result{Blob: b, Commit: selected.Hash.String(), Ref: ref.String(), Digest: archiveDigest}
		return nil
	})
	if err != nil && archivePath != "" {
		if removeErr := removeIgnoringMissing(archivePath); removeErr != nil {
			slog.WarnContext(ctx, "failed to remove incomplete git archive", "path", archivePath, "err", removeErr)
		}
	}
	return result, err
}

// WithRepository supplies the selected commit, the full name of the selecting ref and the
// original Git objects to fn. A ref given by a short name is reported by the full name it
// matched in the repository (a branch first, then a tag); HEAD is reported as the branch it
// points to. A pinned commit selects the commit even if the ref names another one; the ref
// is then looked up among the refs the repository advertises, and is reported as given if
// none matches.
//
// Git identifies objects by SHA-1, which is not FIPS-approved, and go-git hashes
// with it throughout clone, fetch and tree walks. The callback therefore runs
// outside strict enforcement, so it keeps working with GODEBUG=fips140=only.
// FIPS mode itself stays on, so TLS and SSH still negotiate approved algorithms
// only, and the archive OCM records is digested with SHA-256.
func WithRepository(ctx context.Context, access *accessv1.Git, creds *credsv1.GitCredentials, opts Options, fn func(*git.Repository, *object.Commit, plumbing.ReferenceName) error) error {
	var err error
	fips140.WithoutEnforcement(func() { err = withRepository(ctx, access, creds, opts, fn) })
	return err
}

func withRepository(ctx context.Context, access *accessv1.Git, creds *credsv1.GitCredentials, opts Options, fn func(*git.Repository, *object.Commit, plumbing.ReferenceName) error) error {
	if err := access.Validate(); err != nil {
		return fmt.Errorf("invalid git access: %w", err)
	}

	ep, err := endpoint.Parse(access.Repository)
	if err != nil {
		return fmt.Errorf("cannot address git repository: %w", err)
	}
	clientOptions, err := ClientOptions(ep, creds, opts)
	if err != nil {
		return err
	}

	dir, err := os.MkdirTemp(opts.TempDir, "ocm-git-repository-*")
	if err != nil {
		return fmt.Errorf("cannot create git storage: %w", err)
	}

	// Cleanup failures are logged; the callback owns anything it creates outside dir.
	defer func() {
		if rmErr := os.RemoveAll(dir); rmErr != nil {
			slog.WarnContext(ctx, "failed to remove temporary git storage", "path", dir, "err", rmErr)
		}
	}()

	var repo *git.Repository
	if access.Commit == "" && access.Ref == "HEAD" {
		repo, err = git.PlainCloneContext(ctx, dir, &git.CloneOptions{
			URL:           ep.URL,
			ClientOptions: clientOptions,
			Bare:          true,
			Tags:          git.AllTags,
		})
		if err != nil {
			err = TransportError(ctx, "cannot fetch git repository", err)
		}
	} else {
		repo, err = fetchRepository(ctx, dir, ep.URL, access.Commit, clientOptions)
	}

	if err == nil {
		err = useRepository(ctx, repo, access, clientOptions, fn)
	}
	if repo != nil {
		if closer, ok := repo.Storer.(io.Closer); ok {
			err = errors.Join(err, closer.Close())
		}
	}
	return err
}

// useRepository passes the commit the access selects and the full name of its ref to fn.
func useRepository(ctx context.Context, repo *git.Repository, access *accessv1.Git, clientOptions []client.Option, fn func(*git.Repository, *object.Commit, plumbing.ReferenceName) error) error {
	hash := plumbing.NewHash(access.Commit)
	var ref plumbing.ReferenceName
	var err error
	if access.Commit == "" {
		hash, ref, err = resolveRef(repo, access.Ref)
		if err != nil {
			return fmt.Errorf("cannot resolve git ref: %w", err)
		}
	} else if ref, err = pinnedRefName(ctx, repo, access.Ref, clientOptions); err != nil {
		return err
	}

	selected, err := peelCommit(repo, hash)
	if err != nil {
		return err
	}
	return fn(repo, selected, ref)
}

// pinnedRefName returns the full name of ref for an access with a pinned commit. The commit
// alone was fetched, so a short name or HEAD is looked up among the advertised refs, with
// the precedence of resolveRef. A name the repository does not advertise is returned as
// given: the commit is authoritative and the ref informational.
func pinnedRefName(ctx context.Context, repo *git.Repository, ref string, clientOptions []client.Option) (plumbing.ReferenceName, error) {
	if ref == "" || strings.HasPrefix(ref, "refs/") {
		return plumbing.ReferenceName(ref), nil
	}
	remote, err := repo.Remote("origin")
	if err != nil {
		return "", fmt.Errorf("cannot read git remote: %w", err)
	}
	advertised, err := remote.ListContext(ctx, &git.ListOptions{ClientOptions: clientOptions})
	if err != nil {
		return "", TransportError(ctx, "cannot list git refs", err)
	}
	names := make(map[plumbing.ReferenceName]*plumbing.Reference, len(advertised))
	for _, r := range advertised {
		names[r.Name()] = r
	}
	if ref == "HEAD" {
		if head, ok := names[plumbing.HEAD]; ok && head.Type() == plumbing.SymbolicReference {
			return head.Target(), nil
		}
		return plumbing.HEAD, nil
	}
	for _, name := range []plumbing.ReferenceName{plumbing.NewBranchReferenceName(ref), plumbing.NewTagReferenceName(ref)} {
		if _, ok := names[name]; ok {
			return name, nil
		}
	}
	return plumbing.ReferenceName(ref), nil
}

// ClientOptions applies the same credential and HTTP client rules to fetch and push.
func ClientOptions(ep *endpoint.Endpoint, creds *credsv1.GitCredentials, opts Options) ([]client.Option, error) {
	auth, err := authMethod(ep, creds, opts)
	if err != nil {
		return nil, fmt.Errorf("cannot authenticate against git repository: %w", err)
	}
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = ocmhttp.New()
	}
	clientOptions := []client.Option{client.WithHTTPClient(httpClient)}
	if option, ok := authOption(auth); ok {
		clientOptions = append(clientOptions, option)
	}
	return clientOptions, nil
}

// fetchRepository fetches explicit refs or a pinned commit without depending on a valid remote HEAD.
// The repository is returned also with a fetch error, so the caller can close its storage.
func fetchRepository(ctx context.Context, dir, url, commit string, clientOptions []client.Option) (*git.Repository, error) {
	repo, err := git.PlainInit(dir, true)
	if err != nil {
		return nil, fmt.Errorf("cannot create git repository: %w", err)
	}

	if _, err := repo.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{url}}); err != nil {
		return repo, fmt.Errorf("cannot configure git remote: %w", err)
	}

	// Asking for the pinned commit alone avoids transferring every ref and its
	// history. Servers that do not advertise allow-tip-sha1-in-want or
	// allow-reachable-sha1-in-want reject the request before any transfer; the
	// fetch of all refs below is the fallback.
	if commit != "" {
		err = repo.FetchContext(ctx, &git.FetchOptions{
			ClientOptions: clientOptions,
			Tags:          git.NoTags,
			RefSpecs:      []config.RefSpec{config.RefSpec("+" + commit + ":refs/ocm/commit")},
		})
	}
	if commit == "" || errors.Is(err, git.ErrExactSHA1NotSupported) {
		err = repo.FetchContext(ctx, &git.FetchOptions{
			ClientOptions: clientOptions,
			Tags:          git.AllTags,
			RefSpecs:      []config.RefSpec{"+refs/*:refs/*", "+refs/heads/*:refs/remotes/origin/*"},
		})
	}

	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return repo, TransportError(ctx, "cannot fetch git repository", err)
	}

	return repo, nil
}

// resolveRef returns the object name selects and the full name of the ref it matched, as
// the source repository names it: a fetched branch refs/remotes/origin/<name> as
// refs/heads/<name>, HEAD as the branch it points to. It leaves annotated tags intact for
// peelCommit, including tag-to-tag targets.
func resolveRef(repo *git.Repository, name string) (plumbing.Hash, plumbing.ReferenceName, error) {
	if name != "HEAD" && (strings.HasPrefix(name, "refs/heads/") || !strings.HasPrefix(name, "refs/")) {
		ref, err := repo.Reference(plumbing.ReferenceName("refs/remotes/origin/"+strings.TrimPrefix(name, "refs/heads/")), true)
		if err == nil {
			return ref.Hash(), sourceRefName(ref.Name()), nil
		}
	}

	var firstErr error
	for _, rule := range plumbing.RefRevParseRules {
		// Resolving follows symbolic refs, so the name of HEAD is the branch it points to.
		ref, err := repo.Reference(plumbing.ReferenceName(fmt.Sprintf(rule, name)), true)
		if err == nil {
			return ref.Hash(), sourceRefName(ref.Name()), nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}

	return plumbing.ZeroHash, "", firstErr
}

// sourceRefName maps the remote-tracking name a fetched branch is stored under back to
// the name of the branch in the source repository.
func sourceRefName(name plumbing.ReferenceName) plumbing.ReferenceName {
	if branch, ok := strings.CutPrefix(name.String(), "refs/remotes/origin/"); ok {
		return plumbing.NewBranchReferenceName(branch)
	}
	return name
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

// TransportError names the common transport failures and keeps a redacted cause,
// so Git transport failures do not expose repository credentials.
func TransportError(ctx context.Context, operation string, err error) error {
	if ctx.Err() != nil {
		return fmt.Errorf("%s: %w", operation, ctx.Err())
	}

	// The cause is added with %s and not %w: %w prints the error itself, and a
	// transport error quotes the remote URL with its credentials.
	switch {
	case errors.Is(err, git.ErrRepositoryNotExists), errors.Is(err, transport.ErrRepositoryNotFound):
		return fmt.Errorf("%s: repository not found: %s", operation, redact(err))
	case errors.Is(err, transport.ErrAuthenticationRequired):
		return fmt.Errorf("%s: authentication required: %s", operation, redact(err))
	case errors.Is(err, transport.ErrAuthorizationFailed):
		return fmt.Errorf("%s: authorization failed: %s", operation, redact(err))
	default:
		return fmt.Errorf("%s: transport failed; check repository access and server trust: %s", operation, redact(err))
	}
}

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
