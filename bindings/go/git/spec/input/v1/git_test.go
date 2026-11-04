package v1

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	accessv1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestGit_Validate(t *testing.T) {
	for _, tt := range []struct {
		name, repository, ref, commit, wantErr string
	}{
		{
			name:       "remote HEAD",
			repository: "https://example.com/repo.git",
		},
		{
			name:    "missing repository",
			wantErr: "repository must not be empty",
		},
		{
			name:       "invalid ref",
			repository: "https://example.com/repo.git",
			ref:        "refs/heads/../main",
			wantErr:    "invalid git ref",
		},
		{
			name:       "short commit",
			repository: "https://example.com/repo.git",
			commit:     "abc123",
			wantErr:    "40-character hexadecimal SHA",
		},
		{
			name:       "fragment branch",
			repository: "https://example.com/repo.git#branch=main",
		},
		{
			name:       "fragment conflicts with ref",
			repository: "https://example.com/repo.git#branch=main",
			ref:        "dev",
			wantErr:    "conflicts",
		},
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

func TestGit_ToAccess(t *testing.T) {
	commit := strings.Repeat("a", 40)
	for _, tt := range []struct {
		name, repository, ref, commit string
		want                          accessv1.Git
	}{
		{
			name:       "remote HEAD",
			repository: "https://example.com/repo.git",
			want:       accessv1.Git{Repository: "https://example.com/repo.git", Ref: "HEAD"},
		},
		{
			name:       "fields",
			repository: "https://example.com/repo.git",
			ref:        "main",
			commit:     commit,
			want:       accessv1.Git{Repository: "https://example.com/repo.git", Ref: "main", Commit: commit},
		},
		{
			name:       "fragment branch",
			repository: "https://example.com/repo.git#branch=main",
			want:       accessv1.Git{Repository: "https://example.com/repo.git", Ref: "refs/heads/main"},
		},
		{
			name:       "fragment commit",
			repository: "https://example.com/repo.git#commit=" + commit,
			want:       accessv1.Git{Repository: "https://example.com/repo.git", Commit: commit},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			got, err := (&Git{Repository: tt.repository, Ref: tt.ref, Commit: tt.commit}).ToAccess()
			r.NoError(err)
			r.Equal(&tt.want, got)
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
