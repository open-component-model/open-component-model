package v1_test

import (
	"github.com/stretchr/testify/require"
	v1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	for _, tc := range []struct {
		name, repository, ref, commit string
		valid                         bool
	}{
		{"ref", "https://example.com/org/repo.git", "main", "", true},
		{"commit", "https://example.com/org/repo.git", "", strings.Repeat("a", 40), true},
		{"both", "ssh://git@example.com/org/repo.git", "refs/heads/main", strings.Repeat("A", 40), true},
		{"scp", "git@example.com:org/repo.git", "HEAD", "", true},
		{"git", "git://example.com/repo.git", "refs/tags/v1", "", true},
		{"file", "file:///repo.git", "HEAD", "", true},
		{"empty repository", "", "main", "", false},
		{"empty selectors", "https://example.com/repo", "", "", false},
		{"short commit", "https://example.com/repo", "", "abc123", false},
		{"nonhex commit", "https://example.com/repo", "", strings.Repeat("g", 40), false},
		{"invalid ref", "https://example.com/repo", "refs/heads/../main", "", false},
		{"unsupported URL", "ftp://example.com/repo", "main", "", false},
		{"no host", "https:///repo", "main", "", false},
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
