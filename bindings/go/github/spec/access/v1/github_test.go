package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGitHub_Validate(t *testing.T) {
	const validCommit = "0123456789abcdef0123456789abcdef01234567"

	tests := []struct {
		name    string
		github  GitHub
		wantErr string
	}{
		{
			name: "valid with commit only",
			github: GitHub{
				RepoURL: "https://github.com/open-component-model/ocm",
				Commit:  validCommit,
			},
		},
		{
			name: "valid with ref only (resolved and pinned later)",
			github: GitHub{
				RepoURL: "https://github.com/open-component-model/ocm",
				Ref:     "refs/heads/main",
			},
		},
		{
			name: "valid with both commit and ref",
			github: GitHub{
				RepoURL: "https://github.com/open-component-model/ocm",
				Commit:  validCommit,
				Ref:     "refs/heads/main",
			},
		},
		{
			name: "valid with uppercase hex commit",
			github: GitHub{
				RepoURL: "https://github.com/open-component-model/ocm",
				Commit:  "0123456789ABCDEF0123456789ABCDEF01234567",
			},
		},
		{
			name: "valid with enterprise api hostname",
			github: GitHub{
				RepoURL:     "https://github.enterprise.example/org/repo",
				APIHostname: "api.github.enterprise.example",
				Commit:      validCommit,
			},
		},
		{
			name:    "missing repoUrl",
			github:  GitHub{Commit: validCommit},
			wantErr: "repoUrl",
		},
		{
			name: "neither commit nor ref",
			github: GitHub{
				RepoURL: "https://github.com/open-component-model/ocm",
			},
			wantErr: "either commit or ref",
		},
		{
			name: "commit too short",
			github: GitHub{
				RepoURL: "https://github.com/open-component-model/ocm",
				Commit:  "abc123",
			},
			wantErr: "40",
		},
		{
			name: "commit with non-hex characters",
			github: GitHub{
				RepoURL: "https://github.com/open-component-model/ocm",
				Commit:  "z123456789abcdef0123456789abcdef01234567",
			},
			wantErr: "40",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.github.Validate()
			if tt.wantErr == "" {
				assert.NoError(t, err)
			} else {
				assert.ErrorContains(t, err, tt.wantErr)
			}
		})
	}
}
