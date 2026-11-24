// Package git builds throwaway Git repositories for tests.
//
// It is a test-only helper (see the ocm.software/open-component-model/bindings/go/testutils
// package doc): production code must not import it.
package git

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/stretchr/testify/require"
)

// fixtureAuthor keeps fixture commits deterministic so that commit hashes are
// stable across runs and across packages that build the same history.
var fixtureAuthor = object.Signature{
	Name:  "OCM fixture",
	Email: "fixture@example.invalid",
	When:  time.Unix(1700000000, 0).UTC(),
}

// Commit describes one commit to lay down on the fixture history.
type Commit struct {
	// File is the work-tree path written for this commit. Defaults to "README.md".
	File string
	// Content is written to File and, unless Message is set, is also the commit message.
	Content string
	// Message overrides the commit message.
	Message string
}

// Options configures Repository.
type Options struct {
	// Commits is the history to create, oldest first. When nil, Repository writes
	// two commits ("first\n" then "second\n") to README.md.
	Commits []Commit
	// DefaultBranch, when set, is written as the HEAD symbolic reference before the
	// first commit (for example "main").
	DefaultBranch string
	// Branches are created as lightweight references at the first commit.
	Branches []string
	// Tags are created as lightweight references at the first commit.
	Tags []string
	// Bare, when true, bare-clones the work tree into "<tempdir>/fixture.git" and
	// returns that path instead of the work-tree path.
	Bare bool
}

// Repository creates a Git repository fixture under t.TempDir(), applies opts
// and returns the repository path together with the first commit hash. The path
// is the work tree, or the bare clone when Options.Bare is set. Host
// commit.gpgSign is disabled so fixtures are never signed.
func Repository(tb testing.TB, opts Options) (path string, first plumbing.Hash) {
	tb.Helper()
	r := require.New(tb)

	commits := opts.Commits
	if commits == nil {
		commits = []Commit{{Content: "first\n"}, {Content: "second\n"}}
	}

	work := tb.TempDir()
	repo, err := git.PlainInit(work, false)
	r.NoError(err)

	// go-git reads the global Git config; a host commit.gpgSign must not sign fixtures.
	cfg, err := repo.Config()
	r.NoError(err)
	cfg.Commit.GpgSign = config.OptBoolFalse
	r.NoError(repo.SetConfig(cfg))

	if opts.DefaultBranch != "" {
		head := plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName(opts.DefaultBranch))
		r.NoError(repo.Storer.SetReference(head))
	}

	tree, err := repo.Worktree()
	r.NoError(err)

	for i, c := range commits {
		file := c.File
		if file == "" {
			file = "README.md"
		}
		message := c.Message
		if message == "" {
			message = c.Content
		}
		absFile := filepath.Join(work, file)
		r.NoError(os.MkdirAll(filepath.Dir(absFile), 0o755))
		r.NoError(os.WriteFile(absFile, []byte(c.Content), 0o600))
		_, err := tree.Add(file)
		r.NoError(err)
		author := fixtureAuthor
		hash, err := tree.Commit(message, &git.CommitOptions{Author: &author, AllowEmptyCommits: true})
		r.NoError(err)
		if i == 0 {
			first = hash
		}
	}

	for _, branch := range opts.Branches {
		r.NoError(repo.Storer.SetReference(plumbing.NewHashReference(plumbing.NewBranchReferenceName(branch), first)))
	}
	for _, tag := range opts.Tags {
		r.NoError(repo.Storer.SetReference(plumbing.NewHashReference(plumbing.NewTagReferenceName(tag), first)))
	}

	if opts.Bare {
		path = filepath.Join(tb.TempDir(), "fixture.git")
		_, err = git.PlainClone(path, &git.CloneOptions{URL: work, Bare: true, AllowEmptyRepo: len(commits) == 0})
		r.NoError(err)
		return path, first
	}
	return work, first
}
