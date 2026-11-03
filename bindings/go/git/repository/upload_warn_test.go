package repository_test

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/stretchr/testify/require"
)

// warnCapture is a slog.Handler that records Warn records so a test can assert on them.
type warnCapture struct {
	mu      sync.Mutex
	records []slog.Record
}

func (c *warnCapture) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelWarn
}

func (c *warnCapture) Handle(_ context.Context, r slog.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, r)
	return nil
}

func (c *warnCapture) WithAttrs(_ []slog.Attr) slog.Handler { return c }
func (c *warnCapture) WithGroup(_ string) slog.Handler      { return c }

func (c *warnCapture) messages() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	msgs := make([]string, 0, len(c.records))
	for _, r := range c.records {
		if r.Level == slog.LevelWarn {
			msgs = append(msgs, r.Message)
		}
	}
	return msgs
}

func captureWarnings(t *testing.T) *warnCapture {
	t.Helper()
	capture := &warnCapture{}
	orig := slog.Default()
	slog.SetDefault(slog.New(capture))
	t.Cleanup(func() { slog.SetDefault(orig) })
	return capture
}

// TestUploadTagToEmptyRepositoryWarns documents the pitfall a user hit: pushing a tag into
// a fresh repository transfers the history correctly but leaves the target with no branch,
// which Git hosts render as empty. The uploader must still push (no data loss) and must warn
// so the operator understands the result. A branch target into the same repository is silent.
func TestUploadTagToEmptyRepositoryWarns(t *testing.T) {
	fixture := newRepository(t)
	uploader := newUploader(t)
	source, content := downloadResource(t, uploader, fixture.Path, "refs/heads/main", fixture.First)

	for _, tc := range []struct {
		name       string
		targetRef  string
		seedBranch bool
		wantWarn   bool
	}{
		{name: "tag into empty repo warns", targetRef: "refs/tags/rel-x", wantWarn: true},
		{name: "branch into empty repo is silent", targetRef: "refs/heads/main", wantWarn: false},
		{name: "tag into repo with a branch is silent", targetRef: "refs/tags/rel-y", seedBranch: true, wantWarn: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			targetPath := filepath.Join(t.TempDir(), "target.git")
			bare, err := git.PlainInit(targetPath, true)
			r.NoError(err)
			// PlainInit points HEAD at master; hosts point it at the first pushed branch.
			r.NoError(bare.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, "refs/heads/main")))

			if tc.seedBranch {
				// Give the target a branch before the tag push so hasBranch(refs) is true;
				// this is what distinguishes "tag suppressed because a branch exists" from
				// "no warning because the pushed ref is itself a branch".
				_, err := uploader.UploadResource(t.Context(), targetResource(source, targetPath, "refs/heads/main"), content, nil)
				r.NoError(err)
			}

			capture := captureWarnings(t)
			_, err = uploader.UploadResource(t.Context(), targetResource(source, targetPath, tc.targetRef), content, nil)
			r.NoError(err)

			target, err := git.PlainOpen(targetPath)
			r.NoError(err)

			// The push is never lossy: the ref exists and its commit object transferred.
			ref, err := target.Reference(plumbing.ReferenceName(tc.targetRef), false)
			r.NoError(err)
			r.Equal(fixture.First, ref.Hash())
			_, err = target.CommitObject(fixture.First)
			r.NoError(err, "commit object must be present in the target store")

			if tc.wantWarn {
				// The target carries only the tag: no branch for HEAD to resolve.
				branches, err := target.Branches()
				r.NoError(err)
				count := 0
				r.NoError(branches.ForEach(func(*plumbing.Reference) error { count++; return nil }))
				r.Zero(count, "a tag-only push must leave the target without a branch")

				msgs := capture.messages()
				r.NotEmpty(msgs, "pushing a tag into a branchless repository must warn")
				r.Contains(strings.Join(msgs, "\n"), "default branch")
			} else {
				_, err := target.Head()
				r.NoError(err, "a silent case must leave the target with a resolvable HEAD")
				r.Empty(capture.messages(), "pushing a branch must not warn")
			}
		})
	}
}
