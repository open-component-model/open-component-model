package conformance

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/stretchr/testify/require"
)

const ocmv1InputComponentName = "ocm.software/ocmv1-compat-input"

func Test_Integration_OCMv1LocalInputs(t *testing.T) {
	r := require.New(t)

	configPath := newCompatibilityConfig(t)
	inputPath := prepareOCMv1InputCTF(t, configPath)

	inputReference := "ctf::" + inputPath + "//" + ocmv1InputComponentName + ":" + ocmv1Version
	inputDescriptor := readCompatDescriptor(t, configPath, inputReference)
	r.Equal(ocmv1InputComponentName, inputDescriptor.Component.Name)
	r.Len(inputDescriptor.Component.Resources, 2)

	seenInput := make(map[string]bool, 2)
	for _, resource := range inputDescriptor.Component.Resources {
		r.Contains([]string{"helm-input", "git-input"}, resource.Name)
		r.False(seenInput[resource.Name], "duplicate resource %q", resource.Name)
		seenInput[resource.Name] = true
		r.Equal("localBlob", resource.Access["type"])
		r.NotEmpty(resource.Digest["value"])

		t.Run(resource.Name, func(t *testing.T) {
			r := require.New(t)

			if resource.Name == "helm-input" {
				r.Equal("ociArtifactDigest/v1", resource.Digest["normalisationAlgorithm"])
				r.Equal("application/vnd.oci.image.manifest.v1+tar+gzip", resource.Access["mediaType"])
				return
			}

			r.Equal("genericBlobDigest/v1", resource.Digest["normalisationAlgorithm"])
			downloadPath := filepath.Join(t.TempDir(), "download")
			runOCMv2Command(t, configPath, "download", "resource", inputReference,
				"--identity", "name="+resource.Name, "--output", downloadPath, "--extraction-policy", "disable")
			data, err := os.ReadFile(downloadPath)
			r.NoError(err)

			hash := sha256.Sum256(data)
			r.Equal(hex.EncodeToString(hash[:]), resource.Digest["value"])
		})
	}
	r.Len(seenInput, 2)
}

func prepareOCMv1InputCTF(t *testing.T, v1Config string) string {
	t.Helper()
	r := require.New(t)

	workingDirectory := t.TempDir()
	v1Binary := os.Getenv("OCM_V1_BIN")
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
	inputPath := filepath.Join(workingDirectory, "v1-input-ctf")
	runCompatibilityTool(t, v1Binary, "--config", v1Config, "add", "cv", "--create", "--file", inputPath, inputConstructorPath)

	return inputPath
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
