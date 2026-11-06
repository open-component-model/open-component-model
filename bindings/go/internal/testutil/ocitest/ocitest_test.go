package ocitest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/memory"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/internal/testutil/ocitest"
	"ocm.software/open-component-model/bindings/go/oci/spec"
	"ocm.software/open-component-model/bindings/go/oci/spec/annotations"
	"ocm.software/open-component-model/bindings/go/oci/tar"
)

func TestSingleLayerImage(t *testing.T) {
	tests := []struct {
		name string
		refs []string
	}{
		{name: "no ref"},
		{name: "one ref", refs: []string{"ghcr.io/acme/app:1.0.0"}},
		{name: "two refs", refs: []string{"ghcr.io/acme/app:1.0.0", "ghcr.io/acme/app:latest"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			data := []byte("payload")

			layoutTar, access := ocitest.SingleLayerImage(t, data, tt.refs...)

			store, err := tar.ReadOCILayout(t.Context(), inmemory.New(bytes.NewReader(layoutTar)))
			r.NoError(err)
			t.Cleanup(func() { r.NoError(store.Close()) })

			if len(tt.refs) == 0 {
				r.Nil(access)
				return
			}
			r.NotNil(access)
			r.Equal(tt.refs[0], access.ImageReference)

			for _, ref := range tt.refs {
				desc, err := store.Resolve(t.Context(), ref)
				r.NoError(err, "resolve %s", ref)
				r.Equal(ociImageSpecV1.MediaTypeImageManifest, desc.MediaType)

				manifestRaw, err := content.FetchAll(t.Context(), store, desc)
				r.NoError(err)
				var manifest ociImageSpecV1.Manifest
				r.NoError(json.Unmarshal(manifestRaw, &manifest))
				r.Len(manifest.Layers, 1)
				r.Equal(digest.FromBytes(data), manifest.Layers[0].Digest)

				rc, err := store.Fetch(t.Context(), manifest.Layers[0])
				r.NoError(err)
				got, err := io.ReadAll(rc)
				r.NoError(err)
				r.NoError(rc.Close())
				r.Equal(data, got)
			}
		})
	}
}

type memoryResolver struct {
	store *memory.Store
}

func (m memoryResolver) StoreForReference(context.Context, string) (spec.Store, error) {
	return m.store, nil
}

func TestOwnershipReferrers(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()
	store := memory.New()

	subject, err := oras.PackManifest(ctx, store, oras.PackManifestVersion1_1, "application/vnd.test.subject", oras.PackManifestOptions{})
	r.NoError(err)
	r.NoError(store.Tag(ctx, subject, "1.0.0"))

	owner, err := oras.PackManifest(ctx, store, oras.PackManifestVersion1_1, annotations.OwnershipArtifactType, oras.PackManifestOptions{Subject: &subject})
	r.NoError(err)
	_, err = oras.PackManifest(ctx, store, oras.PackManifestVersion1_1, "application/vnd.test.other", oras.PackManifestOptions{Subject: &subject})
	r.NoError(err)

	got := ocitest.OwnershipReferrers(t, ctx, memoryResolver{store: store}, "ghcr.io/acme/app:1.0.0")
	r.Len(got, 1)
	r.Equal(owner.Digest, got[0].Digest)
}
