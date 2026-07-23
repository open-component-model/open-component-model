package sbom

import (
	"context"
	"io"
	"strings"
	"testing"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v1 "ocm.software/open-component-model/bindings/go/input/sbom/spec/v1"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// spdxDoc is a minimal SPDX document; the input method must embed it verbatim
// (no conversion to CycloneDX).
const spdxDoc = `{"spdxVersion":"SPDX-2.3","SPDXID":"SPDXRef-DOCUMENT","name":"podinfo"}`

// fakeDiscoverer returns a fixed set of SBOMs regardless of the resource.
type fakeDiscoverer struct {
	sboms []oci.SBOM
	err   error
}

func (f *fakeDiscoverer) ResolveCredentialConsumerIdentity(_ context.Context, _ *descriptor.Resource) (runtime.Identity, error) {
	return nil, nil
}

func (f *fakeDiscoverer) DiscoverImageSBOMs(_ context.Context, _ *descriptor.Resource, _ runtime.Typed) ([]oci.SBOM, error) {
	return f.sboms, f.err
}

// spdxSBOM builds a discovered SPDX SBOM for the given platform (os/arch).
func spdxSBOM(os, arch string) oci.SBOM {
	var platform *ociImageSpecV1.Platform
	if os != "" || arch != "" {
		platform = &ociImageSpecV1.Platform{OS: os, Architecture: arch}
	}
	return oci.SBOM{
		Blob:      inmemory.New(strings.NewReader(spdxDoc), inmemory.WithMediaType(oci.MediaTypeSPDXJSON)),
		MediaType: oci.MediaTypeSPDXJSON,
		Format:    "spdx",
		Platform:  platform,
	}
}

// newSBOMInputResource builds a constructor resource whose input is SBoM/v1
// referencing the given subject name, with a pre-resolved access and platform.
func newSBOMInputResource(t *testing.T, subject, platform string, access runtime.Typed) *constructorruntime.Resource {
	t.Helper()
	var raw runtime.Raw
	require.NoError(t, runtime.NewScheme(runtime.WithAllowUnknown()).Convert(access, &raw))
	spec := &v1.SBOM{
		Type:     runtime.NewVersionedType(v1.Type, v1.Version),
		Resource: runtime.Identity{constructorruntime.IdentityAttributeName: subject},
		Platform: platform,
		Access:   &raw,
	}
	res := &constructorruntime.Resource{Type: descriptor.ResourceTypeSBOM}
	res.Name = subject + "-sbom"
	res.Version = "0.2.0"
	res.Input = spec
	return res
}

func dummyAccess() runtime.Typed {
	return &runtime.Raw{Type: runtime.NewVersionedType("OCIImage", "v1"), Data: []byte(`{"type":"OCIImage/v1","imageReference":"ghcr.io/stefanprodan/podinfo:6.9.1"}`)}
}

func readAll(t *testing.T, b blob.ReadOnlyBlob) string {
	t.Helper()
	rc, err := b.ReadCloser()
	require.NoError(t, err)
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	return string(data)
}

func mediaTypeOf(t *testing.T, b blob.ReadOnlyBlob) string {
	t.Helper()
	mta, ok := b.(blob.MediaTypeAware)
	require.True(t, ok)
	mt, _ := mta.MediaType()
	return mt
}

func TestProcessResource_SelectsPlatformAndKeepsOriginalFormat(t *testing.T) {
	method := &InputMethod{
		Discoverer: &fakeDiscoverer{sboms: []oci.SBOM{
			spdxSBOM("linux", "amd64"),
			spdxSBOM("linux", "arm64"),
		}},
	}
	res := newSBOMInputResource(t, "podinfo", "linux/arm64", dummyAccess())

	result, err := method.ProcessResource(context.Background(), res, nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.ProcessedBlobData)

	// Original SPDX is preserved verbatim; no CycloneDX conversion.
	assert.Equal(t, spdxDoc, readAll(t, result.ProcessedBlobData))
	assert.Equal(t, oci.MediaTypeSPDXJSON, mediaTypeOf(t, result.ProcessedBlobData))

	// The ocm.software/sbom back-link label was attached, pointing at podinfo.
	label := findLabel(res.Labels, descriptor.LabelSBOM)
	require.NotNil(t, label, "sbom back-link label must be attached")
	var value descriptor.SBOMLabelValue
	require.NoError(t, label.GetValue(&value))
	require.Len(t, value.References, 1)
	assert.Equal(t, "podinfo", value.References[0].Resource[constructorruntime.IdentityAttributeName])
}

func TestProcessResource_SelectsByBareArch(t *testing.T) {
	method := &InputMethod{
		Discoverer: &fakeDiscoverer{sboms: []oci.SBOM{
			spdxSBOM("linux", "amd64"),
			spdxSBOM("linux", "arm64"),
		}},
	}
	res := newSBOMInputResource(t, "podinfo", "arm64", dummyAccess())
	result, err := method.ProcessResource(context.Background(), res, nil)
	require.NoError(t, err)
	assert.Equal(t, spdxDoc, readAll(t, result.ProcessedBlobData))
}

func TestProcessResource_SinglePlatformNeedsNoSelector(t *testing.T) {
	method := &InputMethod{Discoverer: &fakeDiscoverer{sboms: []oci.SBOM{spdxSBOM("", "")}}}
	res := newSBOMInputResource(t, "podinfo", "", dummyAccess())
	result, err := method.ProcessResource(context.Background(), res, nil)
	require.NoError(t, err)
	assert.Equal(t, spdxDoc, readAll(t, result.ProcessedBlobData))
}

func TestProcessResource_ErrorWhenMultiArchWithoutPlatform(t *testing.T) {
	method := &InputMethod{
		Discoverer: &fakeDiscoverer{sboms: []oci.SBOM{
			spdxSBOM("linux", "amd64"),
			spdxSBOM("linux", "arm64"),
		}},
	}
	res := newSBOMInputResource(t, "podinfo", "", dummyAccess())
	_, err := method.ProcessResource(context.Background(), res, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "multi-arch")
	assert.Contains(t, err.Error(), "linux/amd64")
}

func TestProcessResource_ErrorWhenPlatformNotFound(t *testing.T) {
	method := &InputMethod{
		Discoverer: &fakeDiscoverer{sboms: []oci.SBOM{
			spdxSBOM("linux", "amd64"),
		}},
	}
	res := newSBOMInputResource(t, "podinfo", "linux/ppc64le", dummyAccess())
	_, err := method.ProcessResource(context.Background(), res, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no SBOM for platform")
}

func findLabel(labels []constructorruntime.Label, name string) *constructorruntime.Label {
	for i := range labels {
		if labels[i].Name == name {
			return &labels[i]
		}
	}
	return nil
}

func TestProcessResource_ErrorWhenNoAccess(t *testing.T) {
	method := &InputMethod{Discoverer: &fakeDiscoverer{sboms: []oci.SBOM{spdxSBOM("", "")}}}
	res := newSBOMInputResource(t, "podinfo", "", dummyAccess())
	res.Input.(*v1.SBOM).Access = nil

	_, err := method.ProcessResource(context.Background(), res, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no resolved access")
}

func TestProcessResource_ErrorWhenNoSBOMDiscovered(t *testing.T) {
	method := &InputMethod{Discoverer: &fakeDiscoverer{sboms: nil}}
	res := newSBOMInputResource(t, "podinfo", "", dummyAccess())

	_, err := method.ProcessResource(context.Background(), res, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no SBOM discovered")
}

func TestProcessResource_ErrorWhenNoDiscoverer(t *testing.T) {
	method := &InputMethod{}
	res := newSBOMInputResource(t, "podinfo", "", dummyAccess())

	_, err := method.ProcessResource(context.Background(), res, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no image SBOM discoverer")
}

func TestGetResourceCredentialConsumerIdentity_DelegatesToDiscoverer(t *testing.T) {
	method := &InputMethod{Discoverer: &fakeDiscoverer{}}
	res := newSBOMInputResource(t, "podinfo", "", dummyAccess())
	id, err := method.GetResourceCredentialConsumerIdentity(context.Background(), res)
	require.NoError(t, err)
	assert.Nil(t, id)
}
