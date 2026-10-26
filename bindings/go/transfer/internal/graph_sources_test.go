package internal

import (
	"bytes"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ociv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
)

const (
	localSourcesWarning      = "local blob sources are deprecated and heavily discouraged"
	referencedSourcesWarning = "sources are unsafe and cannot be transferred as they lack a digest; copying their references as is"
)

func localBlobSource(name string) descriptor.Source {
	return descriptor.Source{
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: name, Version: "1.0.0"}},
		Type:        "blob",
		Access: &descriptorv2.LocalBlob{
			Type:           runtime.NewVersionedType(descriptorv2.LocalBlobAccessType, descriptorv2.LocalBlobAccessTypeVersion),
			LocalReference: "sha256:abc123",
			MediaType:      "text/plain",
		},
	}
}

func gitSource(name string) descriptor.Source {
	return descriptor.Source{
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: name, Version: "1.0.0"}},
		Type:        "git",
		Access: &runtime.Raw{
			Type: runtime.NewVersionedType("Git", "v1"),
			Data: []byte(`{"type":"Git/v1","repository":"https://github.com/octocat/Hello-World.git","commit":"7fd1a60b01f91b314f59955a4e4d4e80d8edf11d"}`),
		},
	}
}

// captureWarnings routes the default logger into a buffer for the duration of the test.
func captureWarnings(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &buf
}

func uploadComponentMap(t *testing.T, upload map[string]any) map[string]any {
	t.Helper()
	r := require.New(t)
	descSpec, ok := upload["descriptor"].(map[string]any)
	r.True(ok, "descriptor must be composed when a source is transferred")
	component, ok := descSpec["component"].(map[string]any)
	r.True(ok)
	return component
}

func TestBuildGraphDefinition_LocalBlobSourceIsTransferred(t *testing.T) {
	r := require.New(t)
	logs := captureWarnings(t)
	const component, version = "ocm.software/test", "1.0.0"
	sourceRepo := testOCIRepo("ghcr.io/source")
	targetRepo := testCTFRepo("/tmp/target")
	desc := testDescriptor(component, version, nil, nil)
	desc.Component.Sources = []descriptor.Source{localBlobSource("s-input"), gitSource("s-access")}
	resolver := testResolverFor(component, version, sourceRepo, desc)
	roots := testTransferRoots(component, version, targetRepo, resolver)

	tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{}, nil)
	r.NoError(err)

	// One fused transfer for the local blob source, none for the git source, and the upload.
	r.Len(tgd.Transformations, 2)
	transfer, upload := tgd.Transformations[0], tgd.Transformations[1]
	r.Equal(ociv1alpha1.TransferOCILocalSourceToCTFLocalSourceV1alpha1, transfer.Type)
	from, ok := transfer.Spec.Data["source"].(map[string]any)
	r.True(ok)
	r.Equal(component, from["component"])
	r.Equal(version, from["version"])
	r.Equal(map[string]any{"name": "s-input", "version": "1.0.0"}, from["sourceIdentity"])
	r.Contains(upload.ID, "Upload")

	envID := identityToTransformationID(runtime.Identity{
		descriptor.IdentityAttributeName:    component,
		descriptor.IdentityAttributeVersion: version,
	})
	componentMap := uploadComponentMap(t, upload.Spec.Data)
	r.Equal([]any{
		fmt.Sprintf("${%s.output.source}", transfer.ID),
		fmt.Sprintf("${environment.%s.component.sources[1]}", envID),
	}, componentMap["sources"], "the local source references its transfer output, the git source is copied as is")
	r.Nil(componentMap["resources"], "untransformed nil resources stay nil")

	out := logs.String()
	r.Contains(out, localSourcesWarning)
	r.Contains(out, referencedSourcesWarning)
	r.Contains(out, "s-input")
	r.Contains(out, "s-access")
}

func TestBuildGraphDefinition_NonLocalSourceIsCopiedAsIs(t *testing.T) {
	r := require.New(t)
	logs := captureWarnings(t)
	const component, version = "ocm.software/test", "1.0.0"
	sourceRepo := testOCIRepo("ghcr.io/source")
	targetRepo := testOCIRepo("ghcr.io/target")
	desc := testDescriptor(component, version, nil, nil)
	desc.Component.Sources = []descriptor.Source{gitSource("s-access")}
	resolver := testResolverFor(component, version, sourceRepo, desc)
	roots := testTransferRoots(component, version, targetRepo, resolver)

	tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{}, nil)
	r.NoError(err)

	// Nothing is fetched: the environment descriptor is uploaded unchanged.
	r.Len(tgd.Transformations, 1)
	envID := identityToTransformationID(runtime.Identity{
		descriptor.IdentityAttributeName:    component,
		descriptor.IdentityAttributeVersion: version,
	})
	r.Equal(fmt.Sprintf("${environment.%s}", envID), tgd.Transformations[0].Spec.Data["descriptor"])

	out := logs.String()
	r.Contains(out, referencedSourcesWarning)
	r.NotContains(out, localSourcesWarning)
}

func TestBuildGraphDefinition_OnlyLocalSourcesWarnOnce(t *testing.T) {
	r := require.New(t)
	logs := captureWarnings(t)
	const component, version = "ocm.software/test", "1.0.0"
	sourceRepo := testCTFRepo("/tmp/source")
	targetRepo := testOCIRepo("ghcr.io/target")
	desc := testDescriptor(component, version, []descriptor.Resource{localBlobResource("res", version)}, nil)
	desc.Component.Sources = []descriptor.Source{localBlobSource("s-input")}
	resolver := testResolverFor(component, version, sourceRepo, desc)
	roots := testTransferRoots(component, version, targetRepo, resolver)

	tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{}, nil)
	r.NoError(err)

	r.Len(tgd.Transformations, 3)
	r.Equal(ociv1alpha1.TransferCTFLocalBlobToOCILocalBlobV1alpha1, tgd.Transformations[0].Type)
	r.Equal(ociv1alpha1.TransferCTFLocalSourceToOCILocalSourceV1alpha1, tgd.Transformations[1].Type)
	componentMap := uploadComponentMap(t, tgd.Transformations[2].Spec.Data)
	r.Equal([]any{fmt.Sprintf("${%s.output.resource}", tgd.Transformations[0].ID)}, componentMap["resources"])
	r.Equal([]any{fmt.Sprintf("${%s.output.source}", tgd.Transformations[1].ID)}, componentMap["sources"])

	out := logs.String()
	r.Contains(out, localSourcesWarning)
	r.NotContains(out, referencedSourcesWarning)
}

func TestBuildDescriptorSpec_SourcesTransformedOnly(t *testing.T) {
	r := require.New(t)
	v2desc := &descriptorv2.Descriptor{
		Component: descriptorv2.Component{
			Resources: []descriptorv2.Resource{{}},
			Sources:   []descriptorv2.Source{{}, {}},
		},
	}

	spec := buildDescriptorSpec(v2desc, "envID", nil, map[int]string{1: "sourceTransfer"})

	specMap, ok := spec.(map[string]any)
	r.True(ok, "spec should be a map when sources are transformed")
	componentMap, ok := specMap["component"].(map[string]any)
	r.True(ok)
	r.Equal("${environment.envID.component.resources}", componentMap["resources"])
	r.Equal([]any{"${environment.envID.component.sources[0]}", "${sourceTransfer.output.source}"}, componentMap["sources"])
}
