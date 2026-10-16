package v1_test

import (
	"encoding/json"
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
			name:       "fragment not normalized",
			repository: "https://example.com/repo.git#branch=main",
		},
		{
			name:       "fragment not normalized with ref",
			repository: "https://example.com/repo.git#branch=main",
			ref:        "main",
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

func TestUnmarshalJSON(t *testing.T) {
	commit := strings.Repeat("a", 40)
	for _, tc := range []struct {
		name, json string
		want       v1.Git
		wantErr    string
	}{
		{
			name: "without fragment",
			json: `{"type":"Git/v1","repository":"https://example.com/repo.git","ref":"main"}`,
			want: v1.Git{Repository: "https://example.com/repo.git", Ref: "main"},
		},
		{
			name: "branch",
			json: `{"type":"Git/v1","repository":"https://example.com/repo.git#branch=main"}`,
			want: v1.Git{Repository: "https://example.com/repo.git", Ref: "refs/heads/main"},
		},
		{
			name: "tag",
			json: `{"type":"Git/v1","repository":"https://example.com/repo.git#tag=v1.2.3+build.1"}`,
			want: v1.Git{Repository: "https://example.com/repo.git", Ref: "refs/tags/v1.2.3+build.1"},
		},
		{
			name: "commit",
			json: `{"type":"Git/v1","repository":"https://example.com/repo.git#commit=` + commit + `","ref":"main"}`,
			want: v1.Git{Repository: "https://example.com/repo.git", Ref: "main", Commit: commit},
		},
		{
			name: "same branch as ref",
			json: `{"type":"Git/v1","repository":"https://example.com/repo.git#branch=main","ref":"main"}`,
			want: v1.Git{Repository: "https://example.com/repo.git", Ref: "refs/heads/main"},
		},
		{
			name:    "fragment conflicts with ref",
			json:    `{"type":"Git/v1","repository":"https://example.com/repo.git#branch=main","ref":"dev"}`,
			wantErr: "conflicts with",
		},
		{
			name:    "unsupported fragment key",
			json:    `{"type":"Git/v1","repository":"https://example.com/repo.git#rev=main"}`,
			wantErr: "unsupported git repository URL fragment",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			var got v1.Git
			err := json.Unmarshal([]byte(tc.json), &got)
			if tc.wantErr != "" {
				r.ErrorContains(err, tc.wantErr)
				return
			}
			r.NoError(err)
			r.Equal(tc.want.Repository, got.Repository)
			r.Equal(tc.want.Ref, got.Ref)
			r.Equal(tc.want.Commit, got.Commit)
			r.NoError(got.Validate(), "a decoded access is normalized")
		})
	}
}

func TestNormalize(t *testing.T) {
	r := require.New(t)
	in := &v1.Git{Repository: "https://example.com/repo.git#branch=main"}
	got, err := in.Normalize()
	r.NoError(err)
	r.Equal("https://example.com/repo.git", got.Repository)
	r.Equal("refs/heads/main", got.Ref)
	r.Equal("https://example.com/repo.git#branch=main", in.Repository, "Normalize returns a copy")
	r.NoError(got.Validate())
}
