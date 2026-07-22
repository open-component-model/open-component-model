package componentversion

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	sbomspec "ocm.software/open-component-model/bindings/go/input/sbom/spec/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ociAccessRaw builds a Raw OCIImage/v1 access as it appears in the runtime
// constructor after parsing.
func ociAccessRaw(ref string) *runtime.Raw {
	data, _ := json.Marshal(map[string]string{"type": "OCIImage/v1", "imageReference": ref})
	return &runtime.Raw{Type: runtime.NewVersionedType("OCIImage", "v1"), Data: data}
}

// sbomInputRaw builds a Raw SBoM/v1 input referencing the given subject name.
func sbomInputRaw(subject string) *runtime.Raw {
	data, _ := json.Marshal(map[string]any{
		"type":     "SBoM/v1",
		"resource": map[string]string{"name": subject},
	})
	return &runtime.Raw{Type: runtime.NewVersionedType("SBoM", "v1"), Data: data}
}

func componentWith(resources ...constructorruntime.Resource) *constructorruntime.ComponentConstructor {
	comp := constructorruntime.Component{}
	comp.Name = "ocm.software/test"
	comp.Version = "0.2.0"
	comp.Resources = resources
	return &constructorruntime.ComponentConstructor{Components: []constructorruntime.Component{comp}}
}

func resourceWithAccess(name string, access runtime.Typed) constructorruntime.Resource {
	r := constructorruntime.Resource{Type: "ociImage"}
	r.Name = name
	r.Access = access
	return r
}

func resourceWithInput(name string, input runtime.Typed) constructorruntime.Resource {
	r := constructorruntime.Resource{Type: "sbom"}
	r.Name = name
	r.Input = input
	return r
}

func TestResolveSBOMInputs_EmbedsSubjectAccess(t *testing.T) {
	spec := componentWith(
		resourceWithAccess("podinfo", ociAccessRaw("ghcr.io/stefanprodan/podinfo:6.9.1")),
		resourceWithInput("podinfo-sbom", sbomInputRaw("podinfo")),
	)

	require.NoError(t, resolveSBOMInputs(spec))

	input := spec.Components[0].Resources[1].Input
	raw, ok := input.(*runtime.Raw)
	require.True(t, ok)

	var parsed sbomspec.SBOM
	require.NoError(t, json.Unmarshal(raw.Data, &parsed))
	require.NotNil(t, parsed.Access, "subject access must be embedded")
	assert.Equal(t, "OCIImage/v1", parsed.Access.GetType().String())
	assert.Contains(t, string(parsed.Access.Data), "ghcr.io/stefanprodan/podinfo:6.9.1")
}

func TestResolveSBOMInputs_ErrorWhenSubjectMissing(t *testing.T) {
	spec := componentWith(
		resourceWithInput("orphan-sbom", sbomInputRaw("does-not-exist")),
	)
	err := resolveSBOMInputs(spec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist or has no access")
}

func TestResolveSBOMInputs_ErrorWhenSubjectHasNoAccess(t *testing.T) {
	// Subject is itself an input resource (no access), so it can't be discovered.
	spec := componentWith(
		resourceWithInput("local-thing", ociAccessRawAsInput()),
		resourceWithInput("local-thing-sbom", sbomInputRaw("local-thing")),
	)
	err := resolveSBOMInputs(spec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist or has no access")
}

// ociAccessRawAsInput returns a File/v1-like input so the subject resource has an
// input but no access.
func ociAccessRawAsInput() *runtime.Raw {
	data, _ := json.Marshal(map[string]string{"type": "File/v1", "path": "./x"})
	return &runtime.Raw{Type: runtime.NewVersionedType("File", "v1"), Data: data}
}

func TestResolveSBOMInputs_IgnoresNonSBOMInputs(t *testing.T) {
	fileInput := ociAccessRawAsInput()
	spec := componentWith(
		resourceWithInput("cli-sbom", fileInput),
	)
	require.NoError(t, resolveSBOMInputs(spec))
	// Unchanged.
	assert.Equal(t, fileInput, spec.Components[0].Resources[0].Input)
}
