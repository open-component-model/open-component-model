package conformance

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	ocmv1AccessComponentName      = "ocm.software/ocmv1-compat-access"
	ocmv1AccessChildComponentName = "ocm.software/ocmv1-compat-access-child"
	// Gardener descriptors carry a large structured image-vector label on the
	// component and on every reference.
	ocmv1ImageVectorLabel = "imagevector.gardener.cloud/images"
	// Helm resource versions in RBSC transports carry semver build metadata,
	// which is not a valid OCI tag character.
	ocmv1BuildMetadataVersion = "1.2.3+build.456"
)

func Test_Integration_OCMv1AccessDescriptors(t *testing.T) {
	r := require.New(t)

	configPath := newCompatibilityConfig(t)
	accessPath := prepareOCMv1AccessCTF(t, configPath)

	accessReference := "ctf::" + accessPath + "//" + ocmv1AccessComponentName + ":" + ocmv1Version
	accessDescriptor := readCompatDescriptor(t, configPath, accessReference)
	r.Equal(ocmv1AccessComponentName, accessDescriptor.Component.Name)
	r.Equal(ocmv1Version, accessDescriptor.Component.Version)
	r.Len(accessDescriptor.Component.Resources, 5)

	// Shapes taken from the transports users migrate: a structured label on the
	// component and on a reference, a resource type spelled as a media type, a
	// Helm resource version with semver build metadata, and the legacy
	// ociRegistry access spelling next to the current ociArtifact one.
	imageVector := []map[string]any{{
		"name":  ocmv1ImageVectorLabel,
		"value": map[string]any{"images": []any{map[string]any{"name": "sample", "repository": "example.invalid/sample"}}},
	}}
	r.Equal(imageVector, accessDescriptor.Component.Labels)
	r.Len(accessDescriptor.Component.References, 1)
	r.Equal(imageVector, accessDescriptor.Component.References[0].Labels)

	resources := map[string]struct {
		version      string
		resourceType string
		accessType   string
		fields       map[string]any
	}{
		"helm": {
			version:      ocmv1BuildMetadataVersion,
			resourceType: "helmChart/v1",
			accessType:   "helm",
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
		"sbom": {
			resourceType: "application/vnd.cyclonedx+json",
			accessType:   "ociRegistry",
			fields: map[string]any{
				"imageReference": "example.invalid/sample/sbom:1.0.0",
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
			if expected.version != "" {
				r.Equal(expected.version, resource.Version)
			}
			if expected.resourceType != "" {
				r.Equal(expected.resourceType, resource.Type)
			}
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
labels:
  - name: %s
    value:
      images:
        - name: sample
          repository: example.invalid/sample
componentReferences:
  - name: child
    componentName: %s
    version: %s
    labels:
      - name: %s
        value:
          images:
            - name: sample
              repository: example.invalid/sample
resources:
  - name: helm
    version: %s
    type: helmChart/v1
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
  - name: sbom
    version: %s
    type: application/vnd.cyclonedx+json
    access:
      type: ociRegistry
      imageReference: example.invalid/sample/sbom:1.0.0
  - name: s3
    version: %s
    type: blob
    access:
      type: s3
      region: eu-central-1
      bucket: sample-bucket
      key: sample/object.txt
---
name: %s
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
      text: OCM v1 access child fixture
`, ocmv1AccessComponentName, ocmv1Version, ocmv1ImageVectorLabel,
		ocmv1AccessChildComponentName, ocmv1Version, ocmv1ImageVectorLabel,
		ocmv1BuildMetadataVersion, ocmv1Version, ocmv1Version, ocmv1Version, ocmv1Version,
		ocmv1AccessChildComponentName, ocmv1Version, ocmv1Version)
	accessConstructorPath := filepath.Join(workingDirectory, "access-constructor.yaml")
	r.NoError(os.WriteFile(accessConstructorPath, []byte(accessConstructor), 0o600))
	accessPath := filepath.Join(workingDirectory, "v1-access-ctf")
	runCompatibilityTool(t, v1Binary, "--config", v1Config, "add", "cv", "--skip-digest-generation", "--create", "--file", accessPath, accessConstructorPath)

	return accessPath
}
