package v1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestGit_Validate(t *testing.T) {
	for _, tt := range []struct {
		name, repository, ref, commit, wantErr string
	}{
		{"remote HEAD", "https://example.com/repo.git", "", "", ""},
		{"missing repository", "", "", "", "repository must not be empty"},
		{"invalid ref", "https://example.com/repo.git", "refs/heads/../main", "", "invalid git ref"},
		{"short commit", "https://example.com/repo.git", "", "abc123", "40-character hexadecimal SHA"},
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
	for _, typ := range []runtime.Type{
		runtime.NewVersionedType(Type, Version),
		runtime.NewUnversionedType(Type),
		runtime.NewUnversionedType(LegacyType),
		runtime.NewVersionedType(LegacyType, Version),
	} {
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

func TestGit_FetchOptions(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		depth   int
		filter  string
		wantErr string
	}{
		{name: "shallow and partial", depth: 1, filter: "blob:none"},
		{name: "negative depth", depth: -1, wantErr: "depth must not be negative"},
		{name: "unsupported filter", filter: "tree:0", wantErr: "unsupported git filter"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			r := require.New(t)
			spec := &Git{Repository: "https://example.com/repo.git", Depth: testCase.depth, Filter: testCase.filter}
			err := spec.Validate()
			if testCase.wantErr == "" {
				r.NoError(err)
			} else {
				r.ErrorContains(err, testCase.wantErr)
			}
		})
	}
}
