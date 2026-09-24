package internal

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func CreateGitRepository(t *testing.T) (dir, commit string) {
	t.Helper()
	r := require.New(t)
	git, err := exec.LookPath("git")
	r.NoError(err, "git binary should be available in PATH to create the git access repository")

	dir = t.TempDir()
	run := func(args ...string) string {
		args = append([]string{"-C", dir, "-c", "user.name=ocm", "-c", "user.email=ocm@example.invalid", "-c", "commit.gpgsign=false"}, args...)
		out, err := exec.CommandContext(t.Context(), git, args...).CombinedOutput()
		r.NoError(err, string(out))
		return strings.TrimSpace(string(out))
	}

	run("init", "-q", "-b", "main")
	r.NoError(os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello from git access\n"), 0o600))
	run("add", "README.md")
	run("commit", "-q", "-m", "initial")

	return dir, run("rev-parse", "HEAD")
}
