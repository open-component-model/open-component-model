package download

import (
	"context"
	"crypto/fips140"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/client"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/git/internal/endpoint"
	accessv1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
	ocmhttp "ocm.software/open-component-model/bindings/go/http"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Result is one downloaded snapshot of a Git repository with its history, archived as tar.gz.
type Result struct {
	// Commit is the full SHA the archive was taken from.
	Commit string
	// Ref is the full name of the ref the access selects (see Fetch), or empty
	// for an access without ref.
	Ref string
	// Digest covers the final compressed archive bytes.
	Digest digest.Digest
}

// Stream resolves one snapshot of the repository and writes it, archived with its
// history, to w as it is produced. Nothing of the archive is stored.
func Stream(ctx context.Context, access *accessv1.Git, creds runtime.Typed, w io.Writer, opts Options) (_ *Result, err error) {
	clone, err := Fetch(ctx, access, creds, opts)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, clone.Remove(ctx)) }()
	archiveDigest, err := clone.Archive(ctx, w)
	if err != nil {
		return nil, err
	}
	return &Result{Commit: clone.Commit, Ref: clone.Ref, Digest: archiveDigest}, nil
}

// Blob is the archive of a Git access as a lazy blob: every read fetches the repository
// and streams the archive as it is produced, so nothing is stored. The first read that
// completes pins the commit, so later reads produce the same archive even if the ref
// has moved meanwhile. Reads are bound to the context the blob was created with.
//
// A blob made from a [Clone] serves its first read from the clone, without fetching,
// and removes the clone when that reader is closed. If no read takes the clone, it is
// removed when the context ends.
type Blob struct {
	ctx    context.Context
	access *accessv1.Git
	creds  runtime.Typed
	opts   Options

	mu     sync.Mutex
	commit string
	clone  *Clone
	stop   func() bool
}

var (
	_ blob.ReadOnlyBlob   = (*Blob)(nil)
	_ blob.MediaTypeAware = (*Blob)(nil)
)

// NewBlob returns the archive of access as a lazy [Blob]. Errors of the fetch surface on read.
func NewBlob(ctx context.Context, access *accessv1.Git, creds runtime.Typed, opts Options) *Blob {
	return &Blob{ctx: ctx, access: access, creds: creds, opts: opts}
}

// NewBlobFromClone returns the archive of clone as a lazy [Blob] that owns the clone.
// Reads after the first fetch access again, pinned to the commit of the clone.
func NewBlobFromClone(ctx context.Context, clone *Clone, access *accessv1.Git, creds runtime.Typed, opts Options) *Blob {
	b := &Blob{ctx: ctx, access: access, creds: creds, opts: opts, commit: clone.Commit, clone: clone}
	// The lock keeps an already ended context from taking the clone before stop is set.
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stop = context.AfterFunc(ctx, func() {
		if c := b.takeClone(); c != nil {
			// The context has ended, so removal is logged with a fresh one.
			_ = c.Remove(context.WithoutCancel(ctx))
		}
	})
	return b
}

func (b *Blob) ReadCloser() (io.ReadCloser, error) {
	clone := b.takeClone()
	access := b.pinned()
	ctx, cancel := context.WithCancel(b.ctx)
	pr, pw := io.Pipe()
	r := &streamReader{PipeReader: pr, cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(r.done)
		var err error
		if clone != nil {
			_, err = clone.Archive(ctx, pw)
			err = errors.Join(err, clone.Remove(ctx))
		} else {
			var result *Result
			if result, err = Stream(ctx, access, b.creds, pw, b.opts); err == nil {
				b.pin(result.Commit)
			}
		}
		_ = pw.CloseWithError(err)
	}()
	return r, nil
}

// takeClone hands the clone to its only user: the first read or the context ending.
func (b *Blob) takeClone() *Clone {
	b.mu.Lock()
	defer b.mu.Unlock()
	clone := b.clone
	b.clone = nil
	if clone != nil && b.stop != nil {
		b.stop()
	}
	return clone
}

func (b *Blob) MediaType() (string, bool) {
	return MediaType, true
}

// pinned returns the access, pinned to the commit of an earlier read if there was one.
func (b *Blob) pinned() *accessv1.Git {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.commit == "" || b.access.Commit != "" {
		return b.access
	}
	access := *b.access
	access.Commit = b.commit
	return &access
}

func (b *Blob) pin(commit string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.commit == "" {
		b.commit = commit
	}
}

// streamReader reads the archive a download streams. Close stops the download and
// waits until it has removed what it stored, so nothing outlives the reader.
type streamReader struct {
	*io.PipeReader
	cancel context.CancelFunc
	done   chan struct{}
}

func (r *streamReader) Close() error {
	err := r.PipeReader.Close()
	r.cancel()
	<-r.done
	return err
}

// Clone is one fetched snapshot of a repository, kept on disk so its archive can be
// produced without fetching again. The first Archive also keeps the packed Git storage
// it archives, so later archives only tar and compress. The caller removes it with Remove.
type Clone struct {
	// Commit is the full SHA the access selects.
	Commit string
	// Ref is the full name of the selecting ref. A ref given by a short name is reported
	// by the full name it matched in the repository (a branch first, then a tag); HEAD is
	// reported as the branch it points to. A pinned commit selects the commit even if the
	// ref names another one; the ref is then looked up among the refs the repository
	// advertises, and is reported as given if none matches.
	Ref string

	dir      string
	repo     *git.Repository
	selected *object.Commit
	opts     Options
	// packed reports that the Git storage of the archive is written to dir/carrier.
	packed bool
}

// Fetch fetches the snapshot access selects into a temporary directory.
//
// Git identifies objects by SHA-1, which is not FIPS-approved, and go-git hashes
// with it throughout clone, fetch and tree walks. Fetch and Archive therefore run
// outside strict enforcement, so they keep working with GODEBUG=fips140=only.
// FIPS mode itself stays on, so TLS and SSH still negotiate approved algorithms
// only, and the archive OCM records is digested with SHA-256.
func Fetch(ctx context.Context, access *accessv1.Git, creds runtime.Typed, opts Options) (*Clone, error) {
	var clone *Clone
	var err error
	fips140.WithoutEnforcement(func() { clone, err = fetch(ctx, access, creds, opts) })
	return clone, err
}

// Archive writes the archive of the snapshot to w (see archive) and returns its digest.
// A Clone must not archive concurrently.
func (c *Clone) Archive(ctx context.Context, w io.Writer) (digest.Digest, error) {
	var d digest.Digest
	var err error
	fips140.WithoutEnforcement(func() {
		carrier := filepath.Join(c.dir, "carrier")
		if !c.packed {
			if err = writeGitStorage(c.repo, c.selected.Hash, carrier); err != nil {
				err = errors.Join(err, os.RemoveAll(carrier))
				return
			}
			c.packed = true
		}
		d, err = archive(ctx, c.selected, carrier, w, c.opts)
	})
	return d, err
}

// Remove closes the storage and deletes the directory. Failing to delete it is logged.
func (c *Clone) Remove(ctx context.Context) error {
	var err error
	if c.repo != nil {
		if closer, ok := c.repo.Storer.(io.Closer); ok {
			err = closer.Close()
		}
		c.repo = nil
	}
	if rmErr := os.RemoveAll(c.dir); rmErr != nil {
		slog.WarnContext(ctx, "failed to remove temporary git storage", "path", c.dir, "err", rmErr)
	}
	return err
}

func fetch(ctx context.Context, access *accessv1.Git, creds runtime.Typed, opts Options) (_ *Clone, err error) {
	if err := access.Validate(); err != nil {
		return nil, fmt.Errorf("invalid git access: %w", err)
	}
	// go-git appends its request path to the URL, so a fragment must not reach it.
	access, err = access.Normalize()
	if err != nil {
		return nil, fmt.Errorf("invalid git access: %w", err)
	}

	ep, err := endpoint.Parse(access.Repository)
	if err != nil {
		return nil, fmt.Errorf("cannot address git repository: %w", err)
	}
	clientOptions, release, err := ClientOptions(ctx, ep, creds, opts)
	if err != nil {
		return nil, err
	}
	defer release()

	dir, err := os.MkdirTemp(opts.TempDir, "ocm-git-*")
	if err != nil {
		return nil, fmt.Errorf("cannot create git storage: %w", err)
	}
	clone := &Clone{dir: dir, opts: opts}
	defer func() {
		if err != nil {
			err = errors.Join(err, clone.Remove(ctx))
		}
	}()

	if access.Commit == "" && access.Ref == "HEAD" {
		clone.repo, err = git.PlainCloneContext(ctx, filepath.Join(dir, "repository"), &git.CloneOptions{
			URL:           ep.URL,
			ClientOptions: clientOptions,
			Bare:          true,
			Tags:          git.AllTags,
		})
		if err != nil {
			return nil, TransportError(ctx, "cannot fetch git repository", err)
		}
	} else if clone.repo, err = fetchRepository(ctx, filepath.Join(dir, "repository"), ep.URL, access.Commit, clientOptions); err != nil {
		return nil, err
	}

	selected, ref, err := selectCommit(ctx, clone.repo, access, clientOptions)
	if err != nil {
		return nil, err
	}
	clone.selected, clone.Commit, clone.Ref = selected, selected.Hash.String(), ref.String()
	return clone, nil
}

// Pin returns access with the commit its ref currently names on the server, so every
// fetch of the result selects the same snapshot. The refs are listed without fetching
// objects. A ref is resolved like resolveRef: a branch first, then the rev-parse rules;
// HEAD as the commit of the branch it points to, an annotated tag as the commit it
// peels to. An access with a commit is returned normalised and is not looked up.
func Pin(ctx context.Context, access *accessv1.Git, creds runtime.Typed, opts Options) (*accessv1.Git, error) {
	if err := access.Validate(); err != nil {
		return nil, fmt.Errorf("invalid git access: %w", err)
	}
	pinned, err := access.Normalize()
	if err != nil {
		return nil, fmt.Errorf("invalid git access: %w", err)
	}
	if pinned.Commit != "" {
		return pinned, nil
	}

	ep, err := endpoint.Parse(pinned.Repository)
	if err != nil {
		return nil, fmt.Errorf("cannot address git repository: %w", err)
	}
	clientOptions, release, err := ClientOptions(ctx, ep, creds, opts)
	if err != nil {
		return nil, err
	}
	defer release()

	remote := git.NewRemote(memory.NewStorage(), &config.RemoteConfig{Name: "origin", URLs: []string{ep.URL}})
	var advertised []*plumbing.Reference
	fips140.WithoutEnforcement(func() {
		advertised, err = remote.ListContext(ctx, &git.ListOptions{ClientOptions: clientOptions, PeelingOption: git.AppendPeeled})
	})
	if err != nil {
		return nil, TransportError(ctx, "cannot list git refs", err)
	}
	refs := make(map[plumbing.ReferenceName]*plumbing.Reference, len(advertised))
	for _, ref := range advertised {
		refs[ref.Name()] = ref
	}

	hash, ok := advertisedCommit(refs, pinned.Ref)
	if !ok {
		return nil, fmt.Errorf("cannot resolve git ref: %q is not advertised by the repository", pinned.Ref)
	}
	pinned.Commit = hash.String()
	return pinned, nil
}

// advertisedCommit resolves name among advertised refs with the precedence of resolveRef.
// A fetch stores the branches of the server as refs/remotes/origin/*, so those names
// select the branch.
func advertisedCommit(refs map[plumbing.ReferenceName]*plumbing.Reference, name string) (plumbing.Hash, bool) {
	candidates := make([]plumbing.ReferenceName, 0, len(plumbing.RefRevParseRules)+1)
	if name != "HEAD" && (strings.HasPrefix(name, "refs/heads/") || !strings.HasPrefix(name, "refs/")) {
		candidates = append(candidates, plumbing.NewBranchReferenceName(strings.TrimPrefix(name, "refs/heads/")))
	}
	for _, rule := range plumbing.RefRevParseRules {
		candidate := fmt.Sprintf(rule, name)
		if branch, ok := strings.CutPrefix(candidate, "refs/remotes/origin/"); ok {
			candidate = plumbing.NewBranchReferenceName(branch).String()
		}
		candidates = append(candidates, plumbing.ReferenceName(candidate))
	}
	for _, candidate := range candidates {
		ref, ok := refs[candidate]
		// HEAD may be advertised as a symbolic ref; follow it once to its branch.
		if ok && ref.Type() == plumbing.SymbolicReference {
			candidate = ref.Target()
			ref, ok = refs[candidate]
		}
		if !ok || ref.Type() != plumbing.HashReference {
			continue
		}
		// The peeled entry of an annotated tag names the commit it points to.
		if peeled, ok := refs[plumbing.ReferenceName(candidate.String()+"^{}")]; ok {
			return peeled.Hash(), true
		}
		return ref.Hash(), true
	}
	return plumbing.ZeroHash, false
}

// selectCommit returns the commit the access selects and the full name of its ref.
func selectCommit(ctx context.Context, repo *git.Repository, access *accessv1.Git, clientOptions []client.Option) (*object.Commit, plumbing.ReferenceName, error) {
	hash := plumbing.NewHash(access.Commit)
	var ref plumbing.ReferenceName
	var err error
	if access.Commit == "" {
		hash, ref, err = resolveRef(repo, access.Ref)
		if err != nil {
			return nil, "", fmt.Errorf("cannot resolve git ref: %w", err)
		}
	} else if ref, err = pinnedRefName(ctx, repo, access.Ref, clientOptions); err != nil {
		return nil, "", err
	}

	selected, err := peelCommit(repo, hash)
	if err != nil {
		return nil, "", err
	}
	return selected, ref, nil
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
// The caller calls release once it no longer uses the options; it closes the SSH agent
// connection an agent authentication holds.
func ClientOptions(ctx context.Context, ep *endpoint.Endpoint, creds runtime.Typed, opts Options) ([]client.Option, func(), error) {
	auth, err := authMethod(ep, creds, opts)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot authenticate against git repository: %w", err)
	}
	release := func() {}
	if closer, ok := auth.(io.Closer); ok {
		release = func() {
			if closeErr := closer.Close(); closeErr != nil {
				slog.WarnContext(ctx, "failed to close SSH agent connection", "err", closeErr)
			}
		}
	}

	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = ocmhttp.New()
	}
	if _, authenticated := auth.(client.HTTPAuth); ep.Protocol == "https" && (authenticated || ep.User != "" || ep.Password != "") {
		httpClient = authenticatedHTTPClient(httpClient)
	}
	clientOptions := []client.Option{client.WithHTTPClient(httpClient)}
	if option, ok := authOption(auth); ok {
		clientOptions = append(clientOptions, option)
	}
	return clientOptions, release, nil
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
