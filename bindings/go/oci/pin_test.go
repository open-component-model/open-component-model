package oci_test

import (
	"context"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	"ocm.software/open-component-model/bindings/go/repository"
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

func TestRepository_StoredArtifactNotFound(t *testing.T) {
	ctx := t.Context()
	repo := Repository(t, ocictf.WithCTF(newCTFStore(t)), oci.WithComponentVersionLayout(oci.LayoutNormalized))
	require.NoError(t, repo.AddComponentVersion(ctx, normDescriptor()))
	current, err := repo.ResolveComponentVersionDigest(ctx, normComponent, normVersion)
	require.NoError(t, err)

	missing := digest.FromString("no such artifact").String()
	pinned, err := repo.PinComponentVersion(normComponent, normVersion, missing)
	require.NoError(t, err)

	lookups := map[string]func(*oci.Repository, context.Context, string) error{
		"layout": func(repo *oci.Repository, ctx context.Context, version string) error {
			_, err := repo.GetComponentVersionLayout(ctx, normComponent, version)
			return err
		},
		"graph": func(repo *oci.Repository, ctx context.Context, version string) error {
			_, err := repo.DownloadComponentVersionGraph(ctx, normComponent, version)
			return err
		},
	}
	for name, lookup := range lookups {
		t.Run(name+" of a missing version", func(t *testing.T) {
			err := lookup(repo, t.Context(), "2.0.0")
			require.ErrorIs(t, err, repository.ErrNotFound)
			require.ErrorContains(t, err, normComponent+"/2.0.0 not found")
		})
		t.Run(name+" of a missing pinned artifact", func(t *testing.T) {
			err := lookup(pinned.(*oci.Repository), t.Context(), normVersion)
			require.ErrorIs(t, err, repository.ErrNotFound)
			require.ErrorContains(t, err, "does not match pinned digest "+missing)
			require.ErrorContains(t, err, "resolved digest is "+current)
		})
	}
}
