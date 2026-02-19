package e2e

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConformance(t *testing.T) {
	meta := NewConformanceMeta("ref-01", "Reference Conformance Test: Add and Transfer Component Version")
	meta.Bind(t)
	meta.RequireLabel(t, LabelTestKind, ValueConformance)

	// 1. Setup Workspace
	// We use the global shared env.WorkDir for now?
	// Or create a sub-workspace for this test?
	// The providers are global, so Zot and CLI are already running and mounted to env.WorkDir.
	// If we want isolation, we might need nested directories in env.WorkDir.
	// But OCM CLI mount is fixed to specific paths in provider setup.
	// So we must use env.WorkDir.
	// CLEANUP: We should clean up artifacts in env.WorkDir after test or ensure unique names.

	// Copy test data to global workspace
	copyTestData(t, "testdata", env.WorkDir)

	// 8. Generate .ocmconfig (using registry credentials)
	user, pass := env.Registry.GetCredentials()
	// GetURL returns "https://zot:5000", we need to parse if we want specific parts, but here we construct config.
	// Note: Providers are responsible for connectivity.

	ocmConfigContent := fmt.Sprintf(`
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    consumers:
      - identity:
          type: OCIRepository
          hostname: zot
          port: "5000"
          scheme: https
        credentials:
          - type: Credentials/v1
            properties:
              username: %s
              password: %s
`, user, pass)
	require.NoError(t, os.WriteFile(filepath.Join(env.WorkDir, ".ocmconfig"), []byte(ocmConfigContent), 0644))

	// 9. Run Task
	containerID := env.CLI.GetContainerID()
	ocmCmd := fmt.Sprintf("docker exec -w /workspace %s ocm", containerID)

	// Pass variables as arguments to task
	cmd := exec.Command("task", "add-cv", "transfer-cv",
		fmt.Sprintf("OCM_CMD=%s", ocmCmd),
		fmt.Sprintf("TARGET_REPO=%s", env.Registry.GetURL()),
	)
	cmd.Dir = env.WorkDir // Taskfile is copied here
	cmd.Env = os.Environ()

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Task execution failed: %s\nOutput:\n%s", err, string(out))
	} else {
		t.Logf("Task execution succeeded:\n%s", string(out))
	}
}

// copyTestData recursively copies files from src to dst.
func copyTestData(t *testing.T, src, dst string) {
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		targetPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(targetPath, info.Mode())
		}

		srcFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer srcFile.Close()

		dstFile, err := os.Create(targetPath)
		if err != nil {
			return err
		}
		defer dstFile.Close()

		_, err = io.Copy(dstFile, srcFile)
		return err
	})
	require.NoError(t, err)
}
