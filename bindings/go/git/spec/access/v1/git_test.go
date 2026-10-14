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
}
