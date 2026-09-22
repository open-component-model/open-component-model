package v1

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestGit_Validate(t *testing.T) {
	for _, tt := range []struct {
		name, repository, ref, commit, wantErr string
	}{
		{"remote HEAD", "https://example.com/repo.git", "", "", ""},
		{"branch", "https://example.com/repo.git", "main", "", ""},
		{"tag", "git@example.com:repo.git", "refs/tags/v1", "", ""},
		{"explicit HEAD", "file:///repo.git", "HEAD", "", ""},
		{"commit", "https://example.com/repo.git", "", strings.Repeat("a", 40), ""},
		{"both", "ssh://git@example.com/repo.git", "refs/heads/main", strings.Repeat("A", 40), ""},
		{"missing repository", "", "", "", "repository must not be empty"},
		{"invalid repository", "https:///repo.git", "", "", "hostname and path"},
		{"unsupported transport", "ftp://example.com/repo.git", "", "", "unsupported git transport"},
		{"invalid ref", "https://example.com/repo.git", "refs/heads/../main", "", "invalid git ref"},
		{"short commit", "https://example.com/repo.git", "", "abc123", "40-character hexadecimal SHA"},
		{"nonhex commit", "https://example.com/repo.git", "", strings.Repeat("g", 40), "40-character hexadecimal SHA"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			input := Git{Repository: tt.repository, Ref: tt.ref, Commit: tt.commit}
			before := input
			err := input.Validate()
			if tt.wantErr == "" {
				r.NoError(err)
			} else {
				r.ErrorContains(err, tt.wantErr)
			}
			r.Equal(before, input)
			r.Equal(tt.repository, input.String())
		})
	}
}

func TestGit_JSON(t *testing.T) {
	for _, typ := range []runtime.Type{runtime.NewVersionedType(Type, Version), runtime.NewUnversionedType(Type)} {
		t.Run(typ.String(), func(t *testing.T) {
			r := require.New(t)
			input := Git{Type: typ, Repository: "https://example.com/repo.git"}
			data, err := json.Marshal(input)
			r.NoError(err)
			r.JSONEq(`{"type":"`+typ.String()+`","repository":"https://example.com/repo.git"}`, string(data))
			var decoded Git
			r.NoError(json.Unmarshal(data, &decoded))
			r.Equal(input, decoded)
		})
	}
}
