package conformance

import (
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/backend"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/stretchr/testify/require"
)

const legacyGitComponentName = "ocm.software/ocmv1-compat-legacy-git"

// legacyGitResources are the access type spellings for which OCM v1 builds a files-only archive.
var legacyGitResources = []struct {
	name       string
	accessType string
}{
	{name: "git", accessType: "git"},
	{name: "git-v1alpha1", accessType: "git/v1alpha1"},
	{name: "git-capitalised-v1alpha1", accessType: "Git/v1alpha1"},
}

// Test_Integration_OCMv1LegacyGitDigest checks resources that OCM v1 adds with a legacy git
// access type: v1 records the digest of a files-only archive that OCM v2 cannot reproduce,
// so v2 must name the legacy type in the digest mismatch and point to Git/v1.
func Test_Integration_OCMv1LegacyGitDigest(t *testing.T) {
	r := require.New(t)

	configPath := newCompatibilityConfig(t)
	ctfPath := prepareLegacyGitCTF(t, configPath)
	reference := "ctf::" + ctfPath + "//" + legacyGitComponentName + ":" + ocmv1Version

	descriptor := readCompatDescriptor(t, configPath, reference)
	r.Len(descriptor.Component.Resources, len(legacyGitResources))
	for _, resource := range descriptor.Component.Resources {
		r.NotEmpty(resource.Digest["value"], resource.Name)
	}

	for _, tc := range legacyGitResources {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)

			output, err := runLegacyGitOCMv2Failing(t, configPath, "download", "resource", reference,
				"--identity", "name="+tc.name, "--output", filepath.Join(t.TempDir(), "download"),
				"--extraction-policy", "disable")
			r.Error(err, "%s", output)
			r.Contains(string(output), "digest mismatch")
			r.Contains(string(output), fmt.Sprintf("legacy git access type %q", tc.accessType))
			r.Contains(string(output), "re-add the resource with access type Git/v1")
		})
	}

	t.Run("transfer", func(t *testing.T) {
		r := require.New(t)

		output, err := runLegacyGitOCMv2Failing(t, configPath, "transfer", "cv", reference,
			"ctf::"+filepath.Join(t.TempDir(), "target-ctf"), "--copy-resources")
		r.Error(err, "%s", output)
		r.Contains(string(output), "digest mismatch")
		r.Contains(string(output), "the resource uses the legacy git access type")
		r.Contains(string(output), "re-add the resource with access type Git/v1")
	})
}

func prepareLegacyGitCTF(t *testing.T, v1Config string) string {
	t.Helper()
	r := require.New(t)

	// OCM v2 refuses file:// git access, so both CLIs fetch the fixture over loopback HTTP.
	repositoryPath := newGitFixture(t)
	repository, err := git.PlainOpen(repositoryPath)
	r.NoError(err)
	head, err := repository.Head()
	r.NoError(err)
	server := httptest.NewServer(backend.New(transport.MapLoader{"repository.git": repository.Storer}))
	t.Cleanup(server.Close)

	var resources string
	for _, resource := range legacyGitResources {
		resources += fmt.Sprintf(`  - name: %s
    version: %s
    type: directoryTree
    access:
      type: %s
      repository: %s/repository.git
      ref: %s
      commit: %s
`, resource.name, ocmv1Version, resource.accessType, server.URL, head.Name(), head.Hash())
	}
	constructor := fmt.Sprintf("name: %s\nversion: %s\nprovider:\n  name: ocm.software\nresources:\n%s",
		legacyGitComponentName, ocmv1Version, resources)

	workingDirectory := t.TempDir()
	constructorPath := filepath.Join(workingDirectory, "legacy-git-constructor.yaml")
	r.NoError(os.WriteFile(constructorPath, []byte(constructor), 0o600))
	ctfPath := filepath.Join(workingDirectory, "v1-legacy-git-ctf")
	runCompatibilityTool(t, os.Getenv("OCM_V1_BIN"), "--config", v1Config, "add", "cv", "--create", "--file", ctfPath, constructorPath)

	return ctfPath
}

func runLegacyGitOCMv2Failing(t *testing.T, configPath string, arguments ...string) ([]byte, error) {
	t.Helper()
	command := exec.CommandContext(t.Context(), os.Getenv("OCM_V2_BIN"), append(arguments, "--config", configPath, "--loglevel", "error")...)
	return command.CombinedOutput()
}
