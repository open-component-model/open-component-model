package conformance

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/stretchr/testify/require"
)

func prepareOCMv1Scenario(t *testing.T) (signedPath, accessPath, inputPath, publicKey string) {
	t.Helper()
	r := require.New(t)
	workingDirectory := t.TempDir()
	v1Binary := os.Getenv("OCM_V1_BIN")
	v1Config := filepath.Join(workingDirectory, "v1-config.yaml")
	r.NoError(os.WriteFile(v1Config, []byte("type: generic.config.ocm.software/v1\nconfigurations: []\n"), 0o600))
	privateKey := filepath.Join(workingDirectory, "private.pem")
	publicKey = filepath.Join(workingDirectory, "public.pem")
	runCompatibilityTool(t, "openssl", "genpkey", "-algorithm", "RSA", "-pkeyopt", "rsa_keygen_bits:2048", "-out", privateKey)
	runCompatibilityTool(t, "openssl", "pkey", "-in", privateKey, "-pubout", "-out", publicKey)

	signedConstructor := fmt.Sprintf(`name: %s
version: %s
provider:
  name: ocm.software
resources:
  - name: text
    version: %s
    type: plainText
    relation: local
    input:
      type: utf8
      text: OCM v1 compatibility fixture
`, ocmv1ComponentName, ocmv1Version, ocmv1Version)
	signedConstructorPath := filepath.Join(workingDirectory, "signed-constructor.yaml")
	r.NoError(os.WriteFile(signedConstructorPath, []byte(signedConstructor), 0o600))
	signedPath = filepath.Join(workingDirectory, "v1-signed-ctf")
	runCompatibilityTool(t, v1Binary, "--config", v1Config, "add", "cv", "--create", "--file", signedPath, signedConstructorPath)
	v1Reference := signedPath + "//" + ocmv1ComponentName + ":" + ocmv1Version
	runCompatibilityTool(t, v1Binary, "--config", v1Config, "sign", "cv", "--signature", "default", "--private-key", privateKey, v1Reference)
	runCompatibilityTool(t, v1Binary, "--config", v1Config, "verify", "cv", "--signature", "default", "--public-key", publicKey, v1Reference)

	accessConstructor := fmt.Sprintf(`name: %s
version: %s
provider:
  name: ocm.software
resources:
  - name: helm
    version: %s
    type: helmChart
    access:
      type: helm
      helmRepository: https://example.invalid/charts
      helmChart: sample:1.0.0
  - name: oci
    version: %s
    type: ociImage
    access:
      type: ociArtifact
      imageReference: example.invalid/sample/image:1.0.0
  - name: git
    version: %s
    type: directoryTree
    access:
      type: git
      repository: https://example.invalid/sample/repository.git
      ref: refs/heads/main
      commit: 0123456789abcdef0123456789abcdef01234567
  - name: s3
    version: %s
    type: blob
    access:
      type: s3
      region: eu-central-1
      bucket: sample-bucket
      key: sample/object.txt
`, ocmv1AccessComponentName, ocmv1Version, ocmv1Version, ocmv1Version, ocmv1Version, ocmv1Version)
	accessConstructorPath := filepath.Join(workingDirectory, "access-constructor.yaml")
	r.NoError(os.WriteFile(accessConstructorPath, []byte(accessConstructor), 0o600))
	accessPath = filepath.Join(workingDirectory, "v1-access-ctf")
	runCompatibilityTool(t, v1Binary, "--config", v1Config, "add", "cv", "--skip-digest-generation", "--create", "--file", accessPath, accessConstructorPath)

	chartPath := filepath.Join(workingDirectory, "chart")
	r.NoError(os.MkdirAll(chartPath, 0o700))
	r.NoError(os.WriteFile(filepath.Join(chartPath, "Chart.yaml"), []byte("apiVersion: v2\nname: sample\nversion: 1.0.0\n"), 0o600))
	gitPath := newGitFixture(t)
	inputConstructor := fmt.Sprintf(`name: %s
version: %s
provider:
  name: ocm.software
resources:
  - name: helm-input
    version: %s
    type: helmChart
    input:
      type: helm
      path: %q
  - name: git-input
    version: %s
    type: directoryTree
    input:
      type: git
      repository: %q
`, ocmv1InputComponentName, ocmv1Version, ocmv1Version, chartPath, ocmv1Version, "file://"+gitPath)
	inputConstructorPath := filepath.Join(workingDirectory, "input-constructor.yaml")
	r.NoError(os.WriteFile(inputConstructorPath, []byte(inputConstructor), 0o600))
	inputPath = filepath.Join(workingDirectory, "v1-input-ctf")
	runCompatibilityTool(t, v1Binary, "--config", v1Config, "add", "cv", "--create", "--file", inputPath, inputConstructorPath)
	return signedPath, accessPath, inputPath, publicKey
}

func runCompatibilityTool(t *testing.T, binary string, arguments ...string) []byte {
	t.Helper()
	command := exec.CommandContext(t.Context(), binary, arguments...)
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s %v: %s", binary, arguments, output)
	return output
}

func newGitFixture(t *testing.T) string {
	t.Helper()
	r := require.New(t)
	path := t.TempDir()
	repo, err := git.PlainInit(path, false)
	r.NoError(err)
	cfg, err := repo.Config()
	r.NoError(err)
	cfg.Commit.GpgSign = config.OptBoolFalse
	r.NoError(repo.SetConfig(cfg))
	tree, err := repo.Worktree()
	r.NoError(err)
	r.NoError(os.WriteFile(filepath.Join(path, "README.md"), []byte("OCM v1 Git input fixture\n"), 0o600))
	_, err = tree.Add("README.md")
	r.NoError(err)
	_, err = tree.Commit("Add fixture", &git.CommitOptions{Author: &object.Signature{
		Name: "OCM fixture", Email: "fixture@example.invalid", When: time.Unix(1700000000, 0).UTC(),
	}})
	r.NoError(err)
	return path
}
