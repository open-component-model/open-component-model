package internal

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHasEnvKey(t *testing.T) {
	r := require.New(t)
	t.Setenv("SIGSTORE_ID_TOKEN", "some-token")
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "ghs_fakeRunnerToken")
	env := os.Environ()
	r.True(HasEnvKey(env, "SIGSTORE_ID_TOKEN"))
	r.True(HasEnvKey(env, "ACTIONS_ID_TOKEN_REQUEST_TOKEN"))
}

func TestHasEnvKey_EmptyValueTreatedAsAbsent(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	env := []string{"SIGSTORE_ID_TOKEN=", "OTHER_KEY=value"}
	r.False(HasEnvKey(env, "SIGSTORE_ID_TOKEN"))
	r.True(HasEnvKey(env, "OTHER_KEY"))
}

func TestParseCosignVersionOutput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, input, want string
		wantErr           bool
	}{
		{"GitVersion line", "GitVersion:    v3.0.6\n", "v3.0.6", false},
		{"version in other format", "cosign v3.0.3 (linux/amd64)\n", "v3.0.3", false},
		{"no version found", "some random output", "", true},
		{"empty string", "", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			got, err := parseCosignVersionOutput(tc.input)
			if tc.wantErr {
				r.Error(err)
				return
			}
			r.NoError(err)
			r.Equal(tc.want, got)
		})
	}
}
