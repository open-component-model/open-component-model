package oci_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content/memory"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci"
	componentConfig "ocm.software/open-component-model/bindings/go/oci/spec/config/component"
	ocidescriptor "ocm.software/open-component-model/bindings/go/oci/spec/descriptor"
	normalizedlayout "ocm.software/open-component-model/bindings/go/oci/spec/layout/normalized/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func newNormalizedTestDescriptor() *descriptor.Descriptor {
	desc := &descriptor.Descriptor{}
	desc.Component.Name = "example.org/comp"
	desc.Component.Version = "1.0.0"
	desc.Component.Provider = descriptor.Provider{Name: "internal"}
	return desc
}

func TestNormalized_RejectsAdditionalDescriptorManifests(t *testing.T) {
	ctx := context.Background()
	store := memory.New()

	desc := newNormalizedTestDescriptor()

	_, err := oci.AddDescriptorToStore(ctx, store, desc, oci.AddDescriptorOptions{
		Scheme:  runtime.NewScheme(runtime.WithAllowUnknown()),
		Layout:  oci.LayoutNormalized,
		AdditionalDescriptorManifests: []ociImageSpecV1.Descriptor{
			{
				Digest:    "sha256:deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
				MediaType: ociImageSpecV1.MediaTypeImageManifest,
			},
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "additional descriptor manifests")
}

func TestNormalizedLayoutRoundTrip(t *testing.T) {
	ctx := context.Background()
	store := memory.New()

	desc := newNormalizedTestDescriptor()

	mDesc, err := oci.AddDescriptorToStore(ctx, store, desc, oci.AddDescriptorOptions{
		Scheme: runtime.NewScheme(runtime.WithAllowUnknown()),
		Layout: oci.LayoutNormalized,
	})
	require.NoError(t, err)
	require.NotNil(t, mDesc)
	require.Equal(t, ocidescriptor.ArtifactTypeNormalizedDescriptor, mDesc.ArtifactType)
	require.True(t, oci.IsNormalizedManifest(*mDesc))

	got, err := oci.GetNormalizedComponentVersion(ctx, store, *mDesc, ocidescriptor.DefaultDescriptorUnmarshalFunc)
	require.NoError(t, err)
	require.Equal(t, "example.org/comp", got.Component.Name)
	require.Equal(t, "1.0.0", got.Component.Version)
}

// TestNormalizedLayoutSkipsTamperedReferrer verifies that GetNormalizedComponentVersion
// rejects a tampered access manifest (whose descriptor does not re-normalise to the signed
// bytes) and falls through to the legitimate one.
//
// The test is written so that the TAMPERED access manifest's digest sorts LEXICOGRAPHICALLY
// BEFORE the valid one. The read path processes candidates in ascending-digest order, so the
// tampered manifest is tried first. The test can only pass if the bind check fires and the
// code falls through — it is NOT sufficient for the valid manifest to simply sort first.
func TestNormalizedLayoutSkipsTamperedReferrer(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	scheme := runtime.NewScheme(runtime.WithAllowUnknown())

	desc := newNormalizedTestDescriptor()

	mDesc, err := oci.AddDescriptorToStore(ctx, store, desc, oci.AddDescriptorOptions{
		Scheme: scheme,
		Layout: oci.LayoutNormalized,
	})
	require.NoError(t, err)

	// Determine the digest of the valid access manifest that was just written so we know
	// what we need to beat lexicographically.
	predecessors, err := store.Predecessors(ctx, *mDesc)
	require.NoError(t, err)
	var validAccessDesc ociImageSpecV1.Descriptor
	for _, p := range predecessors {
		// The access manifest is the one referrer with ArtifactTypeAccessDescriptor.
		if p.ArtifactType == ocidescriptor.ArtifactTypeAccessDescriptor {
			validAccessDesc = p
			break
		}
	}
	require.NotEmpty(t, validAccessDesc.Digest, "could not find valid access manifest among predecessors")

	// Build a TAMPERED access manifest whose descriptor digest sorts BEFORE the valid one.
	// We iterate over candidate tampered component names until the tampered manifest's digest
	// compares less than the valid manifest's digest string. This is robust: we never hardcode
	// a lucky value — the loop finds one deterministically within very few iterations.
	const maxAttempts = 1000
	var (
		tamperedAccessDesc ociImageSpecV1.Descriptor
		tamperedAccessRaw  []byte
		tamperedLayer      ociImageSpecV1.Descriptor
		tamperedLayerBytes []byte
		tamperedConfigRaw  []byte
		tamperedConfigDesc ociImageSpecV1.Descriptor
		tamperedName       string
	)
	found := false
	for i := 0; i < maxAttempts; i++ {
		tamperedName = fmt.Sprintf("example.org/evil-%d", i)
		evil := newNormalizedTestDescriptor()
		evil.Component.Name = tamperedName // differs from valid in a signed field → bind check fails

		evilBuf, err := ocidescriptor.SingleFileEncodeDescriptor(scheme, evil, ocidescriptor.MediaTypeComponentDescriptorJSON)
		require.NoError(t, err)
		evilBytes := evilBuf.Bytes()
		evilLayerDesc := ociImageSpecV1.Descriptor{
			MediaType: ocidescriptor.MediaTypeComponentDescriptorJSON,
			Digest:    digest.FromBytes(evilBytes),
			Size:      int64(len(evilBytes)),
		}

		cfgRaw, cfgDesc, err := componentConfig.New(evilLayerDesc)
		require.NoError(t, err)

		evilManifest := normalizedlayout.BuildAccessManifest(*mDesc, cfgDesc, evilLayerDesc, nil, tamperedName, evil.Component.Version)
		evilRaw, err := json.Marshal(evilManifest)
		require.NoError(t, err)
		evilDesc := ociImageSpecV1.Descriptor{
			MediaType:    evilManifest.MediaType,
			ArtifactType: evilManifest.ArtifactType,
			Digest:       digest.FromBytes(evilRaw),
			Size:         int64(len(evilRaw)),
			Annotations:  evilManifest.Annotations,
		}

		if evilDesc.Digest.String() < validAccessDesc.Digest.String() {
			// Found a tampered descriptor that will be tried FIRST by the read path.
			tamperedAccessDesc = evilDesc
			tamperedAccessRaw = evilRaw
			tamperedLayer = evilLayerDesc
			tamperedLayerBytes = evilBytes
			tamperedConfigRaw = cfgRaw
			tamperedConfigDesc = cfgDesc
			found = true
			break
		}
	}
	require.True(t, found,
		"could not find a tampered access manifest digest that sorts before the valid one in %d attempts; "+
			"the test logic needs revisiting", maxAttempts)

	// Push all blobs for the winning tampered manifest.
	require.NoError(t, store.Push(ctx, tamperedLayer, bytes.NewReader(tamperedLayerBytes)))
	require.NoError(t, store.Push(ctx, tamperedConfigDesc, bytes.NewReader(tamperedConfigRaw)))
	require.NoError(t, store.Push(ctx, tamperedAccessDesc, bytes.NewReader(tamperedAccessRaw)))

	// The tampered access manifest now sorts BEFORE the valid one. GetNormalizedComponentVersion
	// MUST run the bind check (which rejects it) and fall through to the valid manifest.
	// If the bind check were removed, this assertion would fail because the tampered name
	// "example.org/evil-*" would be returned instead of "example.org/comp".
	got, err := oci.GetNormalizedComponentVersion(ctx, store, *mDesc, ocidescriptor.DefaultDescriptorUnmarshalFunc)
	require.NoError(t, err)
	require.Equal(t, "example.org/comp", got.Component.Name,
		"bind check must reject the tampered referrer (digest %s, sorts before valid %s) "+
			"and fall through to the legitimate one",
		tamperedAccessDesc.Digest, validAccessDesc.Digest)
	require.Equal(t, "1.0.0", got.Component.Version)
}
