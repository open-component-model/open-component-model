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
		{"ref", "https://example.com/org/repo.git", "main", "", true},
		{"commit", "https://example.com/org/repo.git", "", strings.Repeat("a", 40), true},
		{"both", "ssh://git@example.com/org/repo.git", "refs/heads/main", strings.Repeat("A", 40), true},
		{"empty repository", "", "main", "", false},
		{"empty selectors", "https://example.com/repo", "", "", false},
		{"short commit", "https://example.com/repo", "", "abc123", false},
		{"nonhex commit", "https://example.com/repo", "", strings.Repeat("g", 40), false},
		{"invalid ref", "https://example.com/repo", "refs/heads/../main", "", false},
		{"unsupported URL", "ftp://example.com/repo", "main", "", false},
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

func TestFetchOptions(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		depth   int
		filter  string
		wantErr string
	}{
		{name: "full fetch"},
		{name: "shallow fetch", depth: 1},
		{name: "partial fetch", filter: "blob:none"},
		{name: "negative depth", depth: -1, wantErr: "depth must not be negative"},
		{name: "unsupported filter", filter: "tree:0", wantErr: "unsupported git filter"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			r := require.New(t)
			spec := &v1.Git{Repository: "https://example.com/repo.git", Ref: "refs/heads/main", Depth: testCase.depth, Filter: testCase.filter}
			err := spec.Validate()
			if testCase.wantErr == "" {
				r.NoError(err)
			} else {
				r.ErrorContains(err, testCase.wantErr)
			}
		})
	}
}
