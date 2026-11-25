package oci_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
)

func TestRepository_GetComponentVersionLayout_HonorsPin(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()
	ctfStore := newCTFStore(t)
	normalized := Repository(t, ocictf.WithCTF(ctfStore), oci.WithComponentVersionLayout(oci.LayoutNormalized))
	v2Repo := Repository(t, ocictf.WithCTF(ctfStore))

	r.NoError(normalized.AddComponentVersion(ctx, normDescriptor()))
	normalizedDigest, err := normalized.ResolveComponentVersionDigest(ctx, normComponent, normVersion)
	r.NoError(err)

	// Moves the version tag to a manifest in the default layout; the normalized one stays stored.
	r.NoError(v2Repo.AddComponentVersion(ctx, withSignature(normDescriptor(), "ocm-sig")))
	movedDigest, err := v2Repo.ResolveComponentVersionDigest(ctx, normComponent, normVersion)
	r.NoError(err)
	r.NotEqual(normalizedDigest, movedDigest)

	got, err := v2Repo.GetComponentVersionLayout(ctx, normComponent, normVersion)
	r.NoError(err)
	r.Equal(string(layout.ComponentVersionLayoutDefault), got)

	pinned, err := v2Repo.PinComponentVersion(normComponent, normVersion, normalizedDigest)
	r.NoError(err)
	got, err = pinned.(*oci.Repository).GetComponentVersionLayout(ctx, normComponent, normVersion)
	r.NoError(err)
	r.Equal(string(layout.ComponentVersionLayoutNormalized), got, "the layout of the pinned artifact, not of the version tag")

	// A pin on another component does not affect the lookup.
	other, err := v2Repo.PinComponentVersion("example.org/other", normVersion, normalizedDigest)
	r.NoError(err)
	got, err = other.(*oci.Repository).GetComponentVersionLayout(ctx, normComponent, normVersion)
	r.NoError(err)
	r.Equal(string(layout.ComponentVersionLayoutDefault), got)
}
