package internal

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
)

func testComponent(name, version string) *descriptor.Component {
	return &descriptor.Component{
		ComponentMeta: descriptor.ComponentMeta{
			ObjectMeta: descriptor.ObjectMeta{Name: name, Version: version},
		},
	}
}

func TestComponentLabel(t *testing.T) {
	r := require.New(t)
	r.Equal("my-app@1.0.0", componentLabel(testComponent("ocm.software/my-app", "1.0.0")))
	r.Equal("plain@2.0.0", componentLabel(testComponent("plain", "2.0.0")))
	r.Equal("leaf@3.0.0", componentLabel(testComponent("a/very/deep/name/leaf", "3.0.0")))
}

func TestUploadLabel(t *testing.T) {
	r := require.New(t)
	r.Equal("my-app@1.0.0 [Upload to OCI]", uploadLabel(testComponent("ocm.software/my-app", "1.0.0"), testOCIRepo("ghcr.io/t"), 0, 1))
	r.Equal("my-app@1.0.0 [Upload to OCI → target 1]", uploadLabel(testComponent("ocm.software/my-app", "1.0.0"), testOCIRepo("ghcr.io/t"), 0, 2))
	r.Equal("my-app@1.0.0 [Upload to OCI → target 2]", uploadLabel(testComponent("ocm.software/my-app", "1.0.0"), testOCIRepo("ghcr.io/t"), 1, 2))
}

func TestTargetKind(t *testing.T) {
	r := require.New(t)
	r.Equal("OCI", targetKind(testOCIRepo("ghcr.io/t")))
	r.Equal("CTF", targetKind(testCTFRepo("./out")))

	// Raw specs must resolve to the concrete type before the short name is decided.
	data, err := json.Marshal(testOCIRepo("ghcr.io/t"))
	r.NoError(err)
	raw := &runtime.Raw{Type: runtime.Type{Name: oci.Type, Version: oci.Version}, Data: data}
	r.Equal("OCI", targetKind(raw))
}

func TestResourceLabels(t *testing.T) {
	r := require.New(t)
	c := testComponent("ocm.software/test", "0.1.0")
	ociTgt := testOCIRepo("ghcr.io/t")
	ctfTgt := testCTFRepo("./out")

	r.Equal("test@0.1.0 [Get myapp]", getLabel(c, "myapp"))
	r.Equal("test@0.1.0 [Add icons as LocalBlob to CTF]", addLabel(c, "icons", "LocalBlob", ctfTgt))
	r.Equal("test@0.1.0 [Add icons as OCIArtifact to OCI]", addLabel(c, "icons", "OCIArtifact", ociTgt))
	r.Equal("test@0.1.0 [Convert chart to OCIArtifact]", convertLabel(c, "chart"))
	r.Equal("test@0.1.0 [Transfer icons to OCI]", transferLabel(c, "icons", ociTgt))
}

// assertLabel checks the label of the transformation with the given ID.
func assertLabel(t *testing.T, r *require.Assertions, tgd *transformv1alpha1.TransformationGraphDefinition, id, want string) {
	t.Helper()
	for _, tr := range tgd.Transformations {
		if tr.ID == id {
			r.Equal(want, tr.Label, "label of transformation %q", tr.ID)
			return
		}
	}
	r.Failf("transformation not found", "no transformation with ID %q", id)
}

func TestBuildGraphDefinition_Labels(t *testing.T) {
	r := require.New(t)
	sourceRepo := testOCIRepo("ghcr.io/source")
	targetRepo := testOCIRepo("ghcr.io/target")
	desc := testDescriptor("ocm.software/my-app", "1.0.0",
		[]descriptor.Resource{localBlobResource("icons", "1.0.0")}, nil)
	resolver := testResolverFor("ocm.software/my-app", "1.0.0", sourceRepo, desc)

	tgd, err := BuildGraphDefinition(t.Context(),
		testTransferRoots("ocm.software/my-app", "1.0.0", targetRepo, resolver),
		transferv1alpha1.Config{CopyMode: transferv1alpha1.CopyModeLocalBlobResources}, nil)
	r.NoError(err)

	r.NotEmpty(tgd.Transformations)
	for _, tr := range tgd.Transformations {
		assert.NotEmpty(t, tr.Label, "transformation %q is missing a label", tr.ID)
	}

	componentID := identityToTransformationID(runtime.Identity{
		descriptor.IdentityAttributeName:    "ocm.software/my-app",
		descriptor.IdentityAttributeVersion: "1.0.0",
	})
	iconsResource := localBlobResource("icons", "1.0.0")
	resourceID := identityToTransformationID(iconsResource.ToIdentity())

	assertLabel(t, r, tgd, componentID+"Upload", "my-app@1.0.0 [Upload to OCI]")
	assertLabel(t, r, tgd, componentID+"Get"+resourceID, "my-app@1.0.0 [Get icons]")
	assertLabel(t, r, tgd, componentID+"Add"+resourceID, "my-app@1.0.0 [Add icons as LocalBlob to OCI]")
	// the cleanup transformation buffers the Add outputs, so it must exist and carry
	// the shared cleanup label
	assertLabel(t, r, tgd, "fileBufferCleanup", cleanupLabel)
}
