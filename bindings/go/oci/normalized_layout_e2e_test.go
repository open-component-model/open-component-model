package oci_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/memory"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci"
	ocidescriptor "ocm.software/open-component-model/bindings/go/oci/spec/descriptor"
	normalizedlayout "ocm.software/open-component-model/bindings/go/oci/spec/layout/normalized/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// e2eDescriptor returns a minimal, valid component descriptor for e2e tests.
func e2eDescriptor() *descriptor.Descriptor {
	d := &descriptor.Descriptor{}
	d.Component.Name = "example.org/comp"
	d.Component.Version = "1.0.0"
	d.Component.Provider = descriptor.Provider{Name: "internal"}
	return d
}

// TestNormalized_StableDigestAcrossStores proves the copy-stability property:
// writing the same descriptor into two independent memory stores produces
// identical normalized manifest digests — the signable digest is invariant
// across registry locations.
func TestNormalized_StableDigestAcrossStores(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme(runtime.WithAllowUnknown())
	opts := oci.AddDescriptorOptions{Scheme: scheme, Layout: oci.LayoutNormalized}

	storeA := memory.New()
	storeB := memory.New()

	desc := e2eDescriptor()

	mDescA, err := oci.AddDescriptorToStore(ctx, storeA, desc, opts)
	require.NoError(t, err, "write to store A must succeed")
	require.NotNil(t, mDescA)

	mDescB, err := oci.AddDescriptorToStore(ctx, storeB, desc, opts)
	require.NoError(t, err, "write to store B must succeed")
	require.NotNil(t, mDescB)

	require.Equal(t, mDescA.Digest, mDescB.Digest,
		"normalized manifest digest must be identical across independent stores (copy-stability)")
	require.Equal(t, mDescA.ArtifactType, ocidescriptor.ArtifactTypeNormalizedDescriptor,
		"returned descriptor must have normalized artifact type")
	require.True(t, oci.IsNormalizedManifest(*mDescA))
}

// TestNormalized_DigestIsAnchorCosignSigns proves that the digest returned by
// AddDescriptorToStore(...LayoutNormalized) is exactly the digest of the manifest
// that BuildNormalizedManifest constructs from the normalized bytes — i.e. the
// thing cosign will sign.
func TestNormalized_DigestIsAnchorCosignSigns(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme(runtime.WithAllowUnknown())

	desc := e2eDescriptor()

	store := memory.New()
	mDesc, err := oci.AddDescriptorToStore(ctx, store, desc, oci.AddDescriptorOptions{
		Scheme: scheme,
		Layout: oci.LayoutNormalized,
	})
	require.NoError(t, err)
	require.NotNil(t, mDesc)

	// Independently compute what the manifest digest should be.
	normBytes, err := normalizedlayout.Normalize(desc)
	require.NoError(t, err)

	normLayer := ociImageSpecV1.Descriptor{
		MediaType: ocidescriptor.MediaTypeComponentDescriptorNormalizedJSON,
		Digest:    digest.FromBytes(normBytes),
		Size:      int64(len(normBytes)),
	}
	m := normalizedlayout.BuildNormalizedManifest(normLayer, "example.org/comp", "1.0.0")
	mRaw, err := json.Marshal(m)
	require.NoError(t, err)

	expectedDigest := digest.FromBytes(mRaw)

	require.Equal(t, expectedDigest.String(), mDesc.Digest.String(),
		"the tag target digest must equal the digest of the manifest built from normalized bytes")
}

// TestNormalized_CopyBetweenStoresPreservesDigestAndReadback verifies that
// after an oras.ExtendedCopyGraph from store A to store B:
//   - the normalized manifest digest in B is identical to the one in A, and
//   - GetNormalizedComponentVersion reads back the correct component/version.
//
// ExtendedCopyGraph follows predecessors (the access referrer is a predecessor
// of the normalized manifest), which exactly mirrors a registry-to-registry
// copy of a signed artifact and its attachments.
func TestNormalized_CopyBetweenStoresPreservesDigestAndReadback(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme(runtime.WithAllowUnknown())

	desc := e2eDescriptor()

	// Write into store A.
	storeA := memory.New()
	mDescA, err := oci.AddDescriptorToStore(ctx, storeA, desc, oci.AddDescriptorOptions{
		Scheme: scheme,
		Layout: oci.LayoutNormalized,
	})
	require.NoError(t, err)
	require.NotNil(t, mDescA)

	normalizedDigest := mDescA.Digest

	// Copy the normalized manifest subtree (manifest + config + layer) AND its
	// access referrer from A into fresh store B using ExtendedCopyGraph.
	// ExtendedCopyGraph copies the node and all predecessors (referrers) of it,
	// which is exactly registry-to-registry transport of a signed artifact with
	// its attached signatures/referrers.
	storeB := memory.New()
	err = oras.ExtendedCopyGraph(ctx, storeA, storeB, *mDescA, oras.ExtendedCopyGraphOptions{})
	require.NoError(t, err, "ExtendedCopyGraph A→B must succeed")

	// Verify that the normalized manifest is present in B with the same digest.
	mDescInB := *mDescA // same descriptor — blobs are content-addressed
	exists, err := storeB.Exists(ctx, mDescInB)
	require.NoError(t, err)
	require.True(t, exists, "normalized manifest must exist in store B after copy")

	require.Equal(t, normalizedDigest, mDescInB.Digest,
		"the normalized manifest digest must be identical in both stores — no location info bleeds in")

	// Also re-tag the access fallback tag in B so GetNormalizedComponentVersion
	// can discover the access manifest via the fallback path (Predecessors already
	// works because ExtendedCopyGraph tracks the predecessor graph).
	//
	// In practice the fallback tag travels separately (it is a registry tag, not
	// a content blob) so we re-create it: resolve it from A and tag it in B.
	fallbackTag := normalizedlayout.AccessFallbackTag(normalizedDigest.String())
	accessDescA, err := storeA.Resolve(ctx, fallbackTag)
	require.NoError(t, err, "fallback tag must be resolvable in store A")
	err = storeB.Tag(ctx, accessDescA, fallbackTag)
	require.NoError(t, err, "tagging access manifest in store B with fallback tag must succeed")

	// Read back the component version from B.
	got, err := oci.GetNormalizedComponentVersion(ctx, storeB, mDescInB, ocidescriptor.DefaultDescriptorUnmarshalFunc)
	require.NoError(t, err, "GetNormalizedComponentVersion must succeed after copy")
	require.Equal(t, "example.org/comp", got.Component.Name, "component name must survive copy")
	require.Equal(t, "1.0.0", got.Component.Version, "component version must survive copy")
	require.Equal(t, normalizedDigest, mDescInB.Digest, "the normalized manifest digest must still equal the one from store A")
}

// TestNormalized_RejectsUndigestedResource verifies that AddDescriptorToStore
// with LayoutNormalized returns an error containing the resource name when a
// resource has no digest (RequireAllResourcesDigested enforcement).
func TestNormalized_RejectsUndigestedResource(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme(runtime.WithAllowUnknown())

	d := e2eDescriptor()
	r := descriptor.Resource{Type: "ociImage"}
	r.Name = "img"
	r.Version = "1.0.0"
	// r.Digest stays nil — must be rejected
	d.Component.Resources = []descriptor.Resource{r}

	_, err := oci.AddDescriptorToStore(ctx, memory.New(), d, oci.AddDescriptorOptions{
		Scheme: scheme,
		Layout: oci.LayoutNormalized,
	})
	require.Error(t, err, "undigested resource must be rejected")
	require.Contains(t, err.Error(), "img",
		"error message must mention the offending resource name")
}

// noneAccess returns a *runtime.Raw that serializes as {"type":"none"}.
// This is the minimal valid access for resources that have no actual content
// location (e.g. signed-but-none-access resources). Using "none" allows the
// v4alpha1 normalizer to handle it without error.
func noneAccess() *runtime.Raw {
	return &runtime.Raw{
		Type: runtime.NewUnversionedType("none"),
		Data: []byte(`{"type":"none"}`),
	}
}

// TestNormalized_AcceptsDigestedResource verifies that a resource with a valid
// SHA-256 digest is accepted by the normalized layout, and that
// GetNormalizedComponentVersion round-trips the resource name correctly.
//
// The resource uses a "none" access type (the OCI-agnostic sentinel) to satisfy
// the v2 serialization requirement that every resource must have a non-nil Access.
func TestNormalized_AcceptsDigestedResource(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme(runtime.WithAllowUnknown())

	d := e2eDescriptor()
	r := descriptor.Resource{Type: "ociImage"}
	r.Name = "img"
	r.Version = "1.0.0"
	r.Digest = &descriptor.Digest{
		HashAlgorithm:          "SHA-256",
		NormalisationAlgorithm: "genericBlobDigest/v1",
		Value:                  "abc",
	}
	// A non-nil access is required by the v2 serialization layer even in the
	// normalized layout. "none" is the canonical sentinel for no-location access.
	r.Access = noneAccess()
	d.Component.Resources = []descriptor.Resource{r}

	store := memory.New()
	mDesc, err := oci.AddDescriptorToStore(ctx, store, d, oci.AddDescriptorOptions{
		Scheme: scheme,
		Layout: oci.LayoutNormalized,
	})
	require.NoError(t, err, "resource with valid digest and none-access must be accepted")
	require.NotNil(t, mDesc)

	got, err := oci.GetNormalizedComponentVersion(ctx, store, *mDesc, ocidescriptor.DefaultDescriptorUnmarshalFunc)
	require.NoError(t, err, "GetNormalizedComponentVersion must succeed")
	require.Len(t, got.Component.Resources, 1, "resource must survive round-trip")
	require.Equal(t, "img", got.Component.Resources[0].Name, "resource name must match")
}
