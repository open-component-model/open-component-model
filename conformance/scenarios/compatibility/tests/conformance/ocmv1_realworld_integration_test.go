package conformance

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	ocmv1RealWorldComponentName      = "ocm.software/ocmv1-compat-real-world"
	ocmv1RealWorldChildComponentName = "ocm.software/ocmv1-compat-real-world-child"
	// Helm resource versions in RBSC transports carry semver build metadata,
	// which is not a valid OCI tag character.
	ocmv1RealWorldChartVersion = "1.2.3+build.456"
	ocmv1RealWorldImageVector  = "imagevector.gardener.cloud/images"
)

// Test_Integration_OCMv1RealWorldDescriptors reads a component version shaped
// like the transports users actually migrate: Gardener's descriptors carry a
// large structured image-vector label on the component and on every reference,
// spell resource types as media types and accesses in their legacy
// (ociRegistry) and versioned (localBlob/v1) forms; RBSC transports carry Helm
// resource versions with semver build metadata.
func Test_Integration_OCMv1RealWorldDescriptors(t *testing.T) {
	r := require.New(t)

	v1Config := newCompatibilityConfig(t)
	repository := filepath.Join(t.TempDir(), "ctf")
	// The external accesses point at a reserved domain, so v1 cannot digest them.
	addOCMv1ComponentVersion(t, v1Config, repository,
		realWorldConstructor(), "--skip-digest-generation")

	configPath := newCompatibilityConfig(t)
	reference := "ctf::" + repository + "//" + ocmv1RealWorldComponentName + ":" + ocmv1Version
	source := readCompatDescriptor(t, configPath, reference)

	r.Equal([]map[string]any{{
		"name":  ocmv1RealWorldImageVector,
		"value": map[string]any{"images": []any{map[string]any{"name": "sample", "repository": "example.invalid/sample", "tag": "1.0.0"}}},
	}}, source.Component.Labels, "the structured image-vector label survives the read")

	r.Len(source.Component.References, 1)
	r.Equal(ocmv1RealWorldImageVector, source.Component.References[0].Labels[0]["name"])

	resources := make(map[string]ocmv1Resource, len(source.Component.Resources))
	for _, resource := range source.Component.Resources {
		resources[resource.Name] = resource
	}
	r.Len(resources, 3)
	r.Equal(ocmv1RealWorldChartVersion, resources["chart"].Version)
	r.Equal("helmChart/v1", resources["chart"].Type)
	r.Equal("helm", resources["chart"].Access["type"])
	r.Equal("ociRegistry", resources["image"].Access["type"], "the legacy OCI access spelling is read")
	r.Equal("application/vnd.cyclonedx+json", resources["sbom"].Type)
	r.Equal("localBlob", resources["sbom"].Access["type"])

	targetPath := filepath.Join(t.TempDir(), "transferred")
	runOCMv2Command(t, configPath, "transfer", "cv", "--recursive", reference, "ctf::"+targetPath)

	targetReference := "ctf::" + targetPath + "//" + ocmv1RealWorldComponentName + ":" + ocmv1Version
	target := readCompatDescriptor(t, configPath, targetReference)
	r.Equal(source.Component.Labels, target.Component.Labels)
	r.Equal(source.Component.References, target.Component.References)

	transferred := make(map[string]ocmv1Resource, len(target.Component.Resources))
	for _, resource := range target.Component.Resources {
		transferred[resource.Name] = resource
	}
	r.Len(transferred, len(resources))
	for name, resource := range resources {
		// v1 skipped digest generation, so the only recorded digest is the one
		// v2 computes for the local blob it copies.
		if name == "sbom" {
			r.Equal("genericBlobDigest/v1", transferred[name].Digest["normalisationAlgorithm"])
			resource.Digest = transferred[name].Digest
			resource.Access = transferred[name].Access
		}
		r.Equal(resource, transferred[name], "resource %q", name)
	}

	childReference := "ctf::" + targetPath + "//" + ocmv1RealWorldChildComponentName + ":" + ocmv1Version
	r.Equal(ocmv1RealWorldChildComponentName, readCompatDescriptor(t, configPath, childReference).Component.Name)

	downloadPath := filepath.Join(t.TempDir(), "sbom.json")
	runOCMv2Command(t, configPath, "download", "resource", targetReference,
		"--identity", "name=sbom", "--output", downloadPath, "--extraction-policy", "disable")
}

func realWorldConstructor() string {
	return fmt.Sprintf(`name: %s
version: %s
provider:
  name: ocm.software
labels:
  - name: %s
    value:
      images:
        - name: sample
          repository: example.invalid/sample
          tag: 1.0.0
componentReferences:
  - name: child
    componentName: %s
    version: %s
    labels:
      - name: %s
        value:
          images:
            - name: child
              repository: example.invalid/child
resources:
  - name: chart
    version: %s
    type: helmChart/v1
    relation: external
    access:
      type: helm
      helmRepository: https://example.invalid/charts
      helmChart: sample:1.2.3
  - name: image
    version: 1.0.0
    type: ociImage
    relation: external
    access:
      type: ociRegistry
      imageReference: example.invalid/sample/image:1.0.0
  - name: sbom
    version: 1.0.0
    type: application/vnd.cyclonedx+json
    relation: local
    input:
      type: utf8
      text: '{"bomFormat":"CycloneDX"}'
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
      text: OCM v1 real world child fixture
`, ocmv1RealWorldComponentName, ocmv1Version, ocmv1RealWorldImageVector,
		ocmv1RealWorldChildComponentName, ocmv1Version, ocmv1RealWorldImageVector,
		ocmv1RealWorldChartVersion,
		ocmv1RealWorldChildComponentName, ocmv1Version, ocmv1Version)
}
