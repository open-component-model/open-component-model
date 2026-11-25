package attestation

import (
	"context"
	"encoding/json/v2"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	artifactref "ocm.software/open-component-model/bindings/go/descriptor/runtime/labels/artifactref/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type fakeComponentDiscoverer struct {
	atts []repository.Attestation
	err  error
}

func (f fakeComponentDiscoverer) DiscoverComponentAttestations(_ context.Context, _, _ string) ([]repository.Attestation, error) {
	return f.atts, f.err
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
	groups, err := DiscoverAll(t.Context(), Request{Descriptor: desc, DiscoverComponent: withRefs}, 4)
	require.NoError(t, err)
	require.Len(t, groups, 1, "the component referrers form one group")
	assert.Empty(t, groups[0].Label, "the component group has an empty label")
	require.Len(t, groups[0].Attestations, 1)

	empty, err := DiscoverAll(t.Context(), Request{Descriptor: desc, DiscoverComponent: fakeComponentDiscoverer{}}, 4)
	require.NoError(t, err)
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

// resourceNamed is a minimal resource identified only by its name.
func resourceNamed(name string) descriptor.Resource {
	return descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: name}},
	}
}

// describes returns res carrying the artifact-references label that declares it describes
// the target identity, the OCM-local attestation signal fromArtifactReferences reads.
func describes(t *testing.T, res descriptor.Resource, target runtime.Identity) descriptor.Resource {
	t.Helper()
	value, err := json.Marshal(artifactref.References{{Identity: target}}, runtime.JSONOptions)
	require.NoError(t, err)
	res.Labels = []descriptor.Label{{Name: artifactref.LabelName, Version: artifactref.Version, Value: value}}
	return res
}

// TestDiscoverAll_FansOutPerResourceInOrder checks that whole-component discovery fans out
// over the resources, keeps the component group first and the resources in descriptor
// order, and drops a resource whose discovery yields nothing. "sbom" declares it describes
// "app", so app's group carries one OCM-local attestation with no plugin or registry.
func TestDiscoverAll_FansOutPerResourceInOrder(t *testing.T) {
	appID := runtime.Identity{"name": "app"}
	desc := &descriptor.Descriptor{Component: descriptor.Component{
		ComponentMeta: descriptor.ComponentMeta{ObjectMeta: descriptor.ObjectMeta{Name: "demo/c", Version: "1.0.0"}},
		Resources: []descriptor.Resource{
			resourceNamed("app"),
			describes(t, resourceNamed("sbom"), appID),
		},
	}}
	comp := fakeComponentDiscoverer{atts: []repository.Attestation{
		{Source: repository.AttestationSourceOCIReferrer, Digest: "sha256:cd", ID: "sha256:compdoc"},
	}}

	groups, err := DiscoverAll(t.Context(), Request{
		Descriptor:        desc,
		DiscoverComponent: comp,
		Logger:            slog.Default(),
	}, 4)
	require.NoError(t, err)
	require.Len(t, groups, 2, "component group plus the one resource described by a sibling")
	assert.Empty(t, groups[0].Label, "component group comes first")
	assert.Equal(t, appID.String(), groups[1].Label, "then the app resource, in descriptor order")
	require.Len(t, groups[1].Attestations, 1)
	assert.Equal(t, repository.AttestationSourceOCMLocal, groups[1].Attestations[0].Source)
}

// TestDiscoverAll_AllSourcesFailErrors checks that when every source fails and nothing is
// discovered, the empty result surfaces as an error instead of a silent empty success.
func TestDiscoverAll_AllSourcesFailErrors(t *testing.T) {
	boom := errors.New("registry unreachable")
	desc := &descriptor.Descriptor{Component: descriptor.Component{
		ComponentMeta: descriptor.ComponentMeta{ObjectMeta: descriptor.ObjectMeta{Name: "demo/c", Version: "1.0.0"}},
	}}

	_, err := DiscoverAll(t.Context(), Request{
		Descriptor:        desc,
		DiscoverComponent: fakeComponentDiscoverer{err: boom},
		Logger:            slog.Default(),
	}, 4)
	require.Error(t, err, "a wholesale failure must not look like an empty success")
	assert.ErrorIs(t, err, boom)
}
