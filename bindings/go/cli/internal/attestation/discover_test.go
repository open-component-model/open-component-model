package attestation

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
)

type fakeComponentDiscoverer struct {
	atts []repository.Attestation
}

func (f fakeComponentDiscoverer) DiscoverComponentAttestations(_ context.Context, _, _ string) ([]repository.Attestation, error) {
	return f.atts, nil
}

// TestDiscover_ComponentMode checks that a nil Resource routes to the component-level
// discoverer and that its results are deduplicated, without touching the resource paths.
func TestDiscover_ComponentMode(t *testing.T) {
	desc := &descriptor.Descriptor{Component: descriptor.Component{
		ComponentMeta: descriptor.ComponentMeta{ObjectMeta: descriptor.ObjectMeta{Name: "demo/c", Version: "1.0.0"}},
	}}
	fake := fakeComponentDiscoverer{atts: []repository.Attestation{
		{Source: repository.AttestationSourceOCIReferrer, Digest: "sha256:cd", ID: "sha256:doc"},
		{Source: repository.AttestationSourceOCIReferrer, Digest: "sha256:cd", ID: "sha256:doc"}, // duplicate
	}}

	out, err := Discover(t.Context(), Request{Descriptor: desc, Resource: nil, DiscoverComponent: fake})
	require.NoError(t, err)
	require.Len(t, out, 1, "component mode returns the component discoverer's results, deduplicated")
	assert.Equal(t, repository.AttestationSourceOCIReferrer, out[0].Source)
}

// TestDiscoverAll_ComponentGroupAndEmptyDrop checks that whole-component discovery yields
// a component-level group when the component artifact carries referrers, and drops it
// (returning nothing) when it does not. A component with no resources exercises only the
// component path, so no plugin wiring is needed.
func TestDiscoverAll_ComponentGroupAndEmptyDrop(t *testing.T) {
	desc := &descriptor.Descriptor{Component: descriptor.Component{
		ComponentMeta: descriptor.ComponentMeta{ObjectMeta: descriptor.ObjectMeta{Name: "demo/c", Version: "1.0.0"}},
	}}

	withRefs := fakeComponentDiscoverer{atts: []repository.Attestation{
		{Source: repository.AttestationSourceOCIReferrer, Digest: "sha256:cd", ID: "sha256:doc"},
	}}
	groups := DiscoverAll(t.Context(), Request{Descriptor: desc, DiscoverComponent: withRefs}, 4)
	require.Len(t, groups, 1, "the component referrers form one group")
	assert.Empty(t, groups[0].Label, "the component group has an empty label")
	require.Len(t, groups[0].Attestations, 1)

	empty := DiscoverAll(t.Context(), Request{Descriptor: desc, DiscoverComponent: fakeComponentDiscoverer{}}, 4)
	assert.Empty(t, empty, "an empty component group is dropped")
}

func TestDedup(t *testing.T) {
	in := []repository.Attestation{
		// Same subject digest and ID discovered twice (once per source): collapses.
		{Source: repository.AttestationSourceOCIReferrer, Digest: "sha256:img", ID: "sha256:doc"},
		{Source: repository.AttestationSourceOCIIndex, Digest: "sha256:img", ID: "sha256:doc"},
		// Different document on the same artifact: kept.
		{Source: repository.AttestationSourceOCIReferrer, Digest: "sha256:img", ID: "sha256:other"},
		// Local resources have no subject digest but distinct IDs: kept.
		{Source: repository.AttestationSourceOCMLocal, Digest: "", ID: "name=app-sbom"},
		{Source: repository.AttestationSourceOCMLocal, Digest: "", ID: "name=app-vex"},
	}
	out := dedup(in)
	assert.Len(t, out, 4)
	assert.Equal(t, repository.AttestationSourceOCIReferrer, out[0].Source, "first occurrence wins")
}

// TestAccessMediaType reads the declared media type of a local blob resource from its
// access metadata, without downloading the blob.
func TestAccessMediaType(t *testing.T) {
	res := &descriptor.Resource{}
	res.Access = &descriptor.LocalBlob{
		Type:      descriptor.GetLocalBlobAccessType(),
		MediaType: "application/spdx+json",
	}
	assert.Equal(t, "application/spdx+json", accessMediaType(res))

	assert.Empty(t, accessMediaType(&descriptor.Resource{}), "no access means no media type")
}
