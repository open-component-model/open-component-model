package e2e

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestConformance(t *testing.T) {
	meta := NewConformanceMeta("ref-01", "Reference Conformance Test: Add and Transfer Component Version")
	meta.Bind(t)
	meta.RequireLabel(t, LabelTestKind, ValueConformance)

	// 1. Setup Workspace
	workDir := t.TempDir()
	certsDir := filepath.Join(workDir, "certs")
	require.NoError(t, os.MkdirAll(certsDir, 0755))

	// 2. Setup Providers
	ctx := t.Context()

	registry := env.NewRegistryProvider(workDir, certsDir)
	require.NoError(t, registry.Setup(ctx))
	t.Cleanup(func() { _ = registry.Teardown(ctx) })

	cluster := env.NewClusterProvider(workDir)
	require.NoError(t, cluster.Setup(ctx))
	t.Cleanup(func() { _ = cluster.Teardown(ctx) })

	controllers := env.NewControllerProvider(workDir, cluster)
	require.NoError(t, controllers.Setup(ctx, certsDir))
	t.Cleanup(func() { _ = controllers.Teardown(ctx) })

	t.Cleanup(func() {
		if t.Failed() {
			out, err := exec.CommandContext(context.Background(), "kubectl", "logs", "-n", "ocm-system", "-l", "control-plane=controller-manager", "--kubeconfig", cluster.GetKubeconfig()).CombinedOutput()
			t.Logf("Controller Logs on Failure:\nErr: %v\n%s", err, string(out))
			outCRs, err := exec.CommandContext(context.Background(), "kubectl", "get", "repository,component,resource,deployer", "-A", "-o", "yaml", "--kubeconfig", cluster.GetKubeconfig()).CombinedOutput()
			t.Logf("Applied CRs on Failure:\nErr: %v\n%s", err, string(outCRs))
		}
	})

	cli := env.NewCLIProvider(workDir, certsDir)
	require.NoError(t, cli.Setup(ctx))
	t.Cleanup(func() { _ = cli.Teardown(ctx) })

	// Copy test data to workspace
	copyTestData(t, "testdata", workDir)

	// 8. Generate .ocmconfig (using registry credentials)
	user, pass := registry.GetCredentials()
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
	require.NoError(t, os.WriteFile(filepath.Join(workDir, ".ocmconfig"), []byte(ocmConfigContent), 0644))

	// 9. Run Task
	containerID := cli.GetContainerID()
	ocmCmd := fmt.Sprintf("docker exec -w /workspace %s ocm", containerID)

	// Pass variables as arguments to task
	cmd := exec.Command("task", "add-cv", "transfer-cv",
		fmt.Sprintf("OCM_CMD=%s", ocmCmd),
		fmt.Sprintf("TARGET_REPO=%s", registry.GetURL()),
	)
	cmd.Dir = workDir // Taskfile is copied here
	cmd.Env = os.Environ()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		t.Fatalf("Task execution failed: %v\nStdout:\n%s\nStderr:\n%s", err, stdout.String(), stderr.String())
	} else {
		t.Logf("Task execution succeeded:\nStdout:\n%s\nStderr:\n%s", stdout.String(), stderr.String())
	}

	// 10. Apply Custom Resources to deploy ConfigMap
	crs := fmt.Sprintf(`---
apiVersion: delivery.ocm.software/v1alpha1
kind: Repository
metadata:
  name: conformance-repository
  namespace: default
spec:
  repositorySpec:
    baseUrl: %s
    type: OCIRegistry
  ocmConfig:
  - kind: Secret
    name: zot-ocmconfig
    namespace: default
  interval: 10m
---
apiVersion: delivery.ocm.software/v1alpha1
kind: Component
metadata:
  name: conformance-component
  namespace: default
spec:
  component: ocm.software/conformance-test-component
  semver: 1.0.0
  repositoryRef:
    name: conformance-repository
  interval: 10m
---
apiVersion: delivery.ocm.software/v1alpha1
kind: Resource
metadata:
  name: conformance-resource
  namespace: default
spec:
  componentRef:
    name: conformance-component
  resource:
    byReference:
      resource:
        name: sample-configmap
  interval: 10m
---
apiVersion: delivery.ocm.software/v1alpha1
kind: Deployer
metadata:
  name: conformance-deployer
  namespace: default
spec:
  resourceRef:
    name: conformance-resource
    namespace: default
`, registry.GetURL())

	// Create Secret with Zot coordinates
	ocmConfigCmd := exec.CommandContext(ctx, "kubectl", "create", "secret", "generic", "zot-ocmconfig",
		"--from-file=.ocmconfig="+filepath.Join(workDir, ".ocmconfig"),
		"-n", "default",
		"--kubeconfig", cluster.GetKubeconfig(),
	)
	if out, err := ocmConfigCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to create ocmconfig secret: %v\nOutput: %s", err, string(out))
	}

	crsFile := filepath.Join(workDir, "crs.yaml")
	require.NoError(t, os.WriteFile(crsFile, []byte(crs), 0644))

	applyCmd := exec.CommandContext(ctx, "kubectl", "apply", "-f", crsFile, "--kubeconfig", cluster.GetKubeconfig())
	if out, err := applyCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to apply CRs: %v\nOutput: %s", err, string(out))
	}

	// 11. Verify ConfigMap deployed
	t.Log("Waiting for ConfigMap to be deployed by controllers...")
	require.Eventually(t, func() bool {
		checkCmd := exec.CommandContext(ctx, "kubectl", "get", "configmap", "sample-configmap", "-n", "default", "--kubeconfig", cluster.GetKubeconfig())
		return checkCmd.Run() == nil
	}, 2*time.Minute, 5*time.Second, "ConfigMap should be deployed by OCM controllers")
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
