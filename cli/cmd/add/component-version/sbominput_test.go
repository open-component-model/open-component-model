package componentversion

import (
	"context"
	"encoding/json"
	"testing"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	sbomspec "ocm.software/open-component-model/bindings/go/input/sbom/spec/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// fakePlatformLister returns a fixed platform set, ignoring the resource.
type fakePlatformLister struct {
	platforms []ociImageSpecV1.Platform
	err       error
}

func (f *fakePlatformLister) ListImagePlatforms(_ context.Context, _ *descriptor.Resource) ([]ociImageSpecV1.Platform, error) {
	return f.platforms, f.err
}

// singlePlatform is a lister that reports a single-platform image (nil -> no split).
var singlePlatform = &fakePlatformLister{platforms: nil}

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

	require.NoError(t, resolveSBOMInputs(context.Background(), spec, singlePlatform))

	require.Len(t, spec.Components[0].Resources, 2)
	input := spec.Components[0].Resources[1].Input
	raw, ok := input.(*runtime.Raw)
	require.True(t, ok)

	var parsed sbomspec.SBOM
	require.NoError(t, json.Unmarshal(raw.Data, &parsed))
	require.NotNil(t, parsed.Access, "subject access must be embedded")
	assert.Equal(t, "OCIImage/v1", parsed.Access.GetType().String())
	assert.Contains(t, string(parsed.Access.Data), "ghcr.io/stefanprodan/podinfo:6.9.1")
}

func TestResolveSBOMInputs_ExpandsMultiArch(t *testing.T) {
	lister := &fakePlatformLister{platforms: []ociImageSpecV1.Platform{
		{OS: "linux", Architecture: "amd64"},
		{OS: "linux", Architecture: "arm64"},
		{OS: "linux", Architecture: "arm", Variant: "v7"},
	}}
	spec := componentWith(
		resourceWithAccess("podinfo", ociAccessRaw("ghcr.io/stefanprodan/podinfo:6.9.1")),
		resourceWithInput("podinfo-sbom", sbomInputRaw("podinfo")),
	)

	require.NoError(t, resolveSBOMInputs(context.Background(), spec, lister))

	// image resource + 3 per-arch sbom resources.
	res := spec.Components[0].Resources
	require.Len(t, res, 4)

	// The three expanded resources keep the name but carry distinct extraIdentity
	// (both on the resource and in the reference's architecture selector).
	seen := map[string]string{} // resource arch -> reference arch selector
	for _, r := range res[1:] {
		assert.Equal(t, "podinfo-sbom", r.Name)
		arch := r.ExtraIdentity["architecture"]
		require.NotEmpty(t, arch, "expanded resource must be arch-tagged")
		assert.Equal(t, "linux", r.ExtraIdentity["os"])
		var parsed sbomspec.SBOM
		require.NoError(t, json.Unmarshal(r.Input.(*runtime.Raw).Data, &parsed))
		require.NotNil(t, parsed.Access)
		seen[arch] = parsed.Resource.Architecture()
	}
	assert.Equal(t, "amd64", seen["amd64"])
	assert.Equal(t, "arm64", seen["arm64"])
	assert.Equal(t, "arm", seen["arm"])
}

func TestResolveSBOMInputs_ExplicitArchNoExpansion(t *testing.T) {
	// An explicit architecture selector pins one arch even if the image is multi-arch.
	data, _ := json.Marshal(map[string]any{
		"type": "SBoM/v1",
		"resource": map[string]any{
			"name":          "podinfo",
			"extraIdentity": map[string]string{"architecture": "amd64"},
		},
	})
	input := &runtime.Raw{Type: runtime.NewVersionedType("SBoM", "v1"), Data: data}
	lister := &fakePlatformLister{platforms: []ociImageSpecV1.Platform{
		{OS: "linux", Architecture: "amd64"}, {OS: "linux", Architecture: "arm64"},
	}}
	spec := componentWith(
		resourceWithAccess("podinfo", ociAccessRaw("ghcr.io/stefanprodan/podinfo:6.9.1")),
		resourceWithInput("podinfo-sbom", input),
	)

	require.NoError(t, resolveSBOMInputs(context.Background(), spec, lister))
	require.Len(t, spec.Components[0].Resources, 2, "explicit architecture must not expand")
}

func TestResolveSBOMInputs_ErrorWhenSubjectMissing(t *testing.T) {
	spec := componentWith(
		resourceWithInput("orphan-sbom", sbomInputRaw("does-not-exist")),
	)
	err := resolveSBOMInputs(context.Background(), spec, singlePlatform)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist or has no access")
}

func TestResolveSBOMInputs_ErrorWhenSubjectHasNoAccess(t *testing.T) {
	spec := componentWith(
		resourceWithInput("local-thing", ociAccessRawAsInput()),
		resourceWithInput("local-thing-sbom", sbomInputRaw("local-thing")),
	)
	err := resolveSBOMInputs(context.Background(), spec, singlePlatform)
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
	require.NoError(t, resolveSBOMInputs(context.Background(), spec, singlePlatform))
	assert.Equal(t, fileInput, spec.Components[0].Resources[0].Input)
}
