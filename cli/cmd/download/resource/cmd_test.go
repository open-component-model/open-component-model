package resource_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/cli/cmd/internal/test"
)

// TestDownloadResource_ExactMatchWinsOverSubset verifies that when multiple resources
// share the same name+version but differ by extraIdentity, an exact identity lookup
// resolves the correct resource rather than erroring or picking the wrong one.
func TestDownloadResource_ExactMatchWinsOverSubset(t *testing.T) {
	tmp := t.TempDir()

	content1 := []byte("resource without extra identity")
	content2 := []byte("resource with architecture=amd64")

	file1 := filepath.Join(tmp, "file1.txt")
	file2 := filepath.Join(tmp, "file2.txt")
	require.NoError(t, os.WriteFile(file1, content1, 0o600))
	require.NoError(t, os.WriteFile(file2, content2, 0o600))

	constructorYAML := fmt.Sprintf(`
name: ocm.software/test
version: 1.0.0
provider:
  name: test
resources:
  - name: my-resource
    version: 1.0.0
    type: blob
    input:
      type: file/v1
      path: %s
  - name: my-resource
    version: 1.0.0
    type: blob
    extraIdentity:
      architecture: amd64
    input:
      type: file/v1
      path: %s
`, file1, file2)

	constructorPath := filepath.Join(tmp, "constructor.yaml")
	require.NoError(t, os.WriteFile(constructorPath, []byte(constructorYAML), 0o600))

	archivePath := filepath.Join(tmp, "archive")
	_, err := test.OCM(t, test.WithArgs("add", "cv",
		"--constructor", constructorPath,
		"--repository", archivePath,
	))
	require.NoError(t, err, "could not build component version")

	ref := archivePath + "//ocm.software/test:1.0.0"

	t.Run("exact match selects amd64 variant", func(t *testing.T) {
		outFile := filepath.Join(t.TempDir(), "out")
		_, err := test.OCM(t, test.WithArgs("download", "resource", ref,
			"--identity", "name=my-resource,version=1.0.0,architecture=amd64",
			"--output", outFile,
		))
		require.NoError(t, err)
		got, err := os.ReadFile(outFile)
		require.NoError(t, err)
		require.Equal(t, string(content2), string(got))
	})

	t.Run("exact match selects base variant", func(t *testing.T) {
		outFile := filepath.Join(t.TempDir(), "out")
		_, err := test.OCM(t, test.WithArgs("download", "resource", ref,
			"--identity", "name=my-resource,version=1.0.0",
			"--output", outFile,
		))
		require.NoError(t, err)
		got, err := os.ReadFile(outFile)
		require.NoError(t, err)
		require.Equal(t, string(content1), string(got))
	})
}

// TestDownloadResource_AmbiguousSubsetMatchFails verifies that a partial identity
// that matches multiple resources returns an error rather than silently picking one.
func TestDownloadResource_AmbiguousSubsetMatchFails(t *testing.T) {
	tmp := t.TempDir()

	file1 := filepath.Join(tmp, "file1.txt")
	file2 := filepath.Join(tmp, "file2.txt")
	require.NoError(t, os.WriteFile(file1, []byte("amd64 content"), 0o600))
	require.NoError(t, os.WriteFile(file2, []byte("arm64 content"), 0o600))

	constructorYAML := fmt.Sprintf(`
name: ocm.software/test
version: 1.0.0
provider:
  name: test
resources:
  - name: my-resource
    version: 1.0.0
    type: blob
    extraIdentity:
      architecture: amd64
    input:
      type: file/v1
      path: %s
  - name: my-resource
    version: 1.0.0
    type: blob
    extraIdentity:
      architecture: arm64
    input:
      type: file/v1
      path: %s
`, file1, file2)

	constructorPath := filepath.Join(tmp, "constructor.yaml")
	require.NoError(t, os.WriteFile(constructorPath, []byte(constructorYAML), 0o600))

	archivePath := filepath.Join(tmp, "archive")
	_, err := test.OCM(t, test.WithArgs("add", "cv",
		"--constructor", constructorPath,
		"--repository", archivePath,
	))
	require.NoError(t, err, "could not build component version")

	ref := archivePath + "//ocm.software/test:1.0.0"

	// name=my-resource matches both as a subset — should fail with "got 2"
	_, err = test.OCM(t, test.WithArgs("download", "resource", ref,
		"--identity", "name=my-resource",
		"--output", filepath.Join(t.TempDir(), "out"),
	))
	require.Error(t, err)
	require.Contains(t, err.Error(), "expected exactly one resource candidate to download, got 2")
}
