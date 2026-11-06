package conformance

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// Known gaps in how OCM v2 reads descriptors that OCM v1 wrote.
// Every case is linked to its issue with knownGap and asserts the
// behaviour v2 shows today, so the fix turns it into a regular case.

const (
	ocmv1MergeLabelComponentName   = "ocm.software/ocmv1-compat-merge-label"
	ocmv1LegacySchemaComponentName = "ocm.software/ocmv1-compat-legacy-schema"
)

// Test_Integration_OCMv1MergeLabel covers the label merge field. v2 has no
// Merge field on its label types, so the field is dropped on read and the
// transferred copy no longer carries it for a v1 consumer.
func Test_Integration_OCMv1MergeLabel(t *testing.T) {
	r := require.New(t)
	knownGap(t, 1371)

	v1Config := newCompatibilityConfig(t)
	repository := filepath.Join(t.TempDir(), "ctf")
	addOCMv1ComponentVersion(t, v1Config, repository, fmt.Sprintf(`name: %s
version: %s
provider:
  name: ocm.software
labels:
  - name: merge-label
    value: initial
    merge:
      algorithm: default
resources:
  - name: text
    version: %s
    type: plainText
    relation: local
    input:
      type: utf8
      text: OCM v1 merge label fixture
`, ocmv1MergeLabelComponentName, ocmv1Version, ocmv1Version))

	v1Reference := repository + "//" + ocmv1MergeLabelComponentName + ":" + ocmv1Version
	source := readOCMv1Descriptor(t, v1Config, v1Reference)
	r.Equal([]map[string]any{{
		"name":  "merge-label",
		"value": "initial",
		"merge": map[string]any{"algorithm": "default"},
	}}, source.Component.Labels, "v1 stores and reads back the merge field")

	configPath := newCompatibilityConfig(t)
	reference := "ctf::" + v1Reference
	r.Equal([]map[string]any{{"name": "merge-label", "value": "initial"}},
		readCompatDescriptor(t, configPath, reference).Component.Labels)

	targetPath := filepath.Join(t.TempDir(), "transferred")
	runOCMv2Command(t, configPath, "transfer", "cv", reference, "ctf::"+targetPath)
	target := readOCMv1Descriptor(t, v1Config, targetPath+"//"+ocmv1MergeLabelComponentName+":"+ocmv1Version)
	r.Equal([]map[string]any{{"name": "merge-label", "value": "initial"}}, target.Component.Labels)
}

// Test_Integration_OCMv1LegacySchema covers descriptors v1 writes with the
// deprecated ocm.software/v3alpha1 schema. v2 decodes them as v2 and fails
// without naming the schema.
func Test_Integration_OCMv1LegacySchema(t *testing.T) {
	r := require.New(t)
	knownGap(t, 1407)

	v1Config := newCompatibilityConfig(t)
	repository := filepath.Join(t.TempDir(), "ctf")
	addOCMv1ComponentVersion(t, v1Config, repository,
		plainTextConstructor(ocmv1LegacySchemaComponentName), "--scheme", "ocm.software/v3alpha1")

	// The v3alpha1 shape has no "component" field, so it is only read as text.
	v1Reference := repository + "//" + ocmv1LegacySchemaComponentName + ":" + ocmv1Version
	v1Output := runCompatibilityTool(t, os.Getenv("OCM_V1_BIN"), "--config", v1Config,
		"get", "cv", "-o", "yaml", v1Reference)
	r.Contains(string(v1Output), "apiVersion: ocm.software/v3alpha1", "v1 reads its own v3alpha1 descriptor")

	configPath := newCompatibilityConfig(t)
	output := runFailingOCMv2Command(t, configPath, "get", "cv", "ctf::"+v1Reference)
	r.Contains(output, `parsing identity "name=,version="`)
	r.NotContains(output, "v3alpha1", "the error does not name the deprecated schema")
}
