package transformation

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDetermineOutputPath(t *testing.T) {
	r := require.New(t)
	base := t.TempDir()
	r.NoError(os.Mkdir(filepath.Join(base, "out"), 0o755))
	r.NoError(os.WriteFile(filepath.Join(base, "file"), []byte("keep"), 0o600))
	t.Chdir(base)
	for _, tc := range []struct{ name, path, wantErr string }{
		{name: "default"},
		{name: "absolute", path: filepath.Join(base, "out")},
		{name: "relative", path: "out"},
		{name: "missing", path: "missing", wantErr: "does not exist"},
		{name: "file", path: "file", wantErr: "is a file, not a directory"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			path, err := determineOutputPath(tc.path, "git-resource")
			if tc.wantErr != "" {
				r.ErrorContains(err, tc.wantErr)
				r.Empty(path)
				return
			}
			r.NoError(err)
			t.Cleanup(func() { _ = os.Remove(path) })
			r.True(filepath.IsAbs(path))
			r.Contains(filepath.Base(path), "git-resource-")
			info, err := os.Stat(path)
			r.NoError(err)
			r.False(info.IsDir())
			if tc.path != "" {
				r.Equal(filepath.Join(base, "out"), filepath.Dir(path))
			}
		})
	}
	content, err := os.ReadFile("file")
	r.NoError(err)
	r.Equal("keep", string(content))
}
