package conformance

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const ocmv1AccessComponentName = "ocm.software/ocmv1-compat-access"

func Test_Integration_OCMv1AccessDescriptors(t *testing.T) {
	r := require.New(t)

	configPath := newCompatibilityConfig(t)
	accessPath := prepareOCMv1AccessCTF(t, configPath)

	accessReference := "ctf::" + accessPath + "//" + ocmv1AccessComponentName + ":" + ocmv1Version
	accessDescriptor := readCompatDescriptor(t, configPath, accessReference)
	r.Equal(ocmv1AccessComponentName, accessDescriptor.Component.Name)
	r.Equal(ocmv1Version, accessDescriptor.Component.Version)
	r.Len(accessDescriptor.Component.Resources, 4)

	resources := map[string]struct {
		accessType string
		fields     map[string]any
	}{
		"helm": {
			accessType: "helm",
			fields: map[string]any{
				"helmRepository": "https://example.invalid/charts",
				"helmChart":      "sample:1.0.0",
			},
		},
		"oci": {
			accessType: "ociArtifact",
			fields: map[string]any{
				"imageReference": "example.invalid/sample/image:1.0.0",
			},
		},
		"git": {
			accessType: "git",
			fields: map[string]any{
				"repository": "https://example.invalid/sample/repository.git",
				"ref":        "refs/heads/main",
				"commit":     "0123456789abcdef0123456789abcdef01234567",
			},
		},
		"s3": {
			accessType: "s3",
			fields: map[string]any{
				"region": "eu-central-1",
				"bucket": "sample-bucket",
				"key":    "sample/object.txt",
			},
		},
	}

	seen := make(map[string]bool, len(resources))
	for _, resource := range accessDescriptor.Component.Resources {
		expected, ok := resources[resource.Name]
		r.True(ok, "unexpected resource %q", resource.Name)
		r.False(seen[resource.Name], "duplicate resource %q", resource.Name)
		seen[resource.Name] = true

		t.Run(resource.Name, func(t *testing.T) {
			r := require.New(t)

			r.Equal(expected.accessType, resource.Access["type"])
			for field, value := range expected.fields {
				r.Equal(value, resource.Access[field], field)
			}
		})
	}
	r.Len(seen, len(resources))
}

func prepareOCMv1AccessCTF(t *testing.T, v1Config string) string {
	t.Helper()
	r := require.New(t)

	workingDirectory := t.TempDir()
	v1Binary := os.Getenv("OCM_V1_BIN")
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
	accessPath := filepath.Join(workingDirectory, "v1-access-ctf")
	runCompatibilityTool(t, v1Binary, "--config", v1Config, "add", "cv", "--skip-digest-generation", "--create", "--file", accessPath, accessConstructorPath)

	return accessPath
}
