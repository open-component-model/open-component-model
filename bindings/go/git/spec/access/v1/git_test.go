package v1_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	v1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
)

func TestValidate(t *testing.T) {
	for _, tc := range []struct {
		name, repository, ref, commit string
		valid                         bool
	}{
		{
			name:       "ref",
			repository: "https://example.com/org/repo.git",
			ref:        "main",
			valid:      true,
		},
		{
			name:       "commit",
			repository: "https://example.com/org/repo.git",
			commit:     strings.Repeat("a", 40),
			valid:      true,
		},
		{
			name:       "both",
			repository: "ssh://git@example.com/org/repo.git",
			ref:        "refs/heads/main",
			commit:     strings.Repeat("A", 40),
			valid:      true,
		},
		{
			name: "empty repository",
			ref:  "main",
		},
		{
			name:       "empty selectors",
			repository: "https://example.com/repo",
		},
		{
			name:       "short commit",
			repository: "https://example.com/repo",
			commit:     "abc123",
		},
		{
			name:       "nonhex commit",
			repository: "https://example.com/repo",
			commit:     strings.Repeat("g", 40),
		},
		{
			name:       "invalid ref",
			repository: "https://example.com/repo",
			ref:        "refs/heads/../main",
		},
		{
			name:       "unsupported URL",
			repository: "ftp://example.com/repo",
			ref:        "main",
		},
		{
			name:       "fragment branch",
			repository: "https://example.com/repo.git#branch=main",
			valid:      true,
		},
		{
			name:       "fragment commit",
			repository: "https://example.com/repo.git#commit=" + strings.Repeat("a", 40),
			valid:      true,
		},
		{
			name:       "fragment short commit",
			repository: "https://example.com/repo.git#commit=abc123",
		},
		{
			name:       "fragment invalid branch",
			repository: "https://example.com/repo.git#branch=..",
		},
		{
			name:       "fragment conflicts with ref",
			repository: "https://example.com/repo.git#branch=main",
			ref:        "dev",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)

			err := (&v1.Git{Repository: tc.repository, Ref: tc.ref, Commit: tc.commit}).Validate()
			if tc.valid {
				r.NoError(err)
			} else {
				r.Error(err)
			}
		})
	}

	t.Run("nil access", func(t *testing.T) {
		var g *v1.Git
		require.Error(t, g.Validate())
	})
}

func TestNormalize(t *testing.T) {
	commit := strings.Repeat("a", 40)
	for _, tc := range []struct {
		name, repository, ref, commit       string
		wantRepository, wantRef, wantCommit string
		err                                 string
	}{
		{
			name:           "no fragment",
			repository:     "https://example.com/repo.git",
			ref:            "main",
			commit:         commit,
			wantRepository: "https://example.com/repo.git",
			wantRef:        "main",
			wantCommit:     commit,
		},
		{
			name:           "branch",
			repository:     "https://example.com/org/repo.git#branch=main",
			wantRepository: "https://example.com/org/repo.git",
			wantRef:        "refs/heads/main",
		},
		{
			name:           "tag on scp-like URL",
			repository:     "git@example.com:org/repo.git#tag=v1.0.0",
			wantRepository: "git@example.com:org/repo.git",
			wantRef:        "refs/tags/v1.0.0",
		},
		{
			name:           "full branch fragment",
			repository:     "https://example.com/repo.git#branch=refs/heads/main",
			wantRepository: "https://example.com/repo.git",
			wantRef:        "refs/heads/main",
		},
		{
			name:           "full tag fragment",
			repository:     "https://example.com/repo.git#tag=refs/tags/v1",
			wantRepository: "https://example.com/repo.git",
			wantRef:        "refs/tags/v1",
		},
		{name: "tag ref as branch", repository: "https://example.com/repo.git#branch=refs/tags/v1", err: "branch fragment requires a branch ref"},
		{name: "branch ref as tag", repository: "https://example.com/repo.git#tag=refs/heads/main", err: "tag fragment requires a tag ref"},
		{
			name:           "commit on local path",
			repository:     "/srv/repo.git#commit=" + commit,
			wantRepository: "/srv/repo.git",
			wantCommit:     commit,
		},
		{
			name:           "branch and commit",
			repository:     "https://example.com/repo.git#branch=release/1.x&commit=" + commit,
			wantRepository: "https://example.com/repo.git",
			wantRef:        "refs/heads/release/1.x",
			wantCommit:     commit,
		},
		{
			name:           "plus in tag",
			repository:     "https://example.com/repo.git#tag=v1.2.3+build.1",
			wantRepository: "https://example.com/repo.git",
			wantRef:        "refs/tags/v1.2.3+build.1",
		},
		{
			name:           "escaped branch",
			repository:     "https://example.com/repo.git#branch=feature%2Fx",
			wantRepository: "https://example.com/repo.git",
			wantRef:        "refs/heads/feature/x",
		},
		{
			name:           "empty fragment",
			repository:     "https://example.com/repo.git#",
			ref:            "main",
			wantRepository: "https://example.com/repo.git",
			wantRef:        "main",
		},
		{
			name:           "same short branch",
			repository:     "https://example.com/repo.git#branch=main",
			ref:            "main",
			wantRepository: "https://example.com/repo.git",
			wantRef:        "refs/heads/main",
		},
		{
			name:           "same short tag",
			repository:     "https://example.com/repo.git#tag=v1",
			ref:            "v1",
			wantRepository: "https://example.com/repo.git",
			wantRef:        "refs/tags/v1",
		},
		{
			name:           "same qualified branch",
			repository:     "https://example.com/repo.git#branch=main",
			ref:            "refs/heads/main",
			wantRepository: "https://example.com/repo.git",
			wantRef:        "refs/heads/main",
		},
		{
			name:           "same commit in other case",
			repository:     "https://example.com/repo.git#commit=" + commit,
			commit:         strings.ToUpper(commit),
			wantRepository: "https://example.com/repo.git",
			wantCommit:     commit,
		},
		{name: "different branch", repository: "https://example.com/repo.git#branch=main", ref: "dev", err: "conflicts"},
		{name: "different commit", repository: "https://example.com/repo.git#commit=" + commit, commit: strings.Repeat("b", 40), err: "conflicts"},
		{name: "unsupported key", repository: "https://example.com/repo.git#ref=main", err: "unsupported git repository URL fragment"},
		{name: "empty value", repository: "https://example.com/repo.git#branch=", err: "requires exactly one value"},
		{name: "repeated key", repository: "https://example.com/repo.git#branch=a&branch=b", err: "requires exactly one value"},
		{name: "branch and tag", repository: "https://example.com/repo.git#branch=main&tag=v1", err: "must not set both branch and tag"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)

			in := &v1.Git{Repository: tc.repository, Ref: tc.ref, Commit: tc.commit}
			got, err := in.Normalize()
			if tc.err != "" {
				r.ErrorContains(err, tc.err)
				return
			}
			r.NoError(err)
			r.Equal(tc.wantRepository, got.Repository)
			r.Equal(tc.wantRef, got.Ref)
			r.Equal(tc.wantCommit, got.Commit)
			r.Equal(tc.repository, in.Repository, "the input is not modified")
		})
	}
}
