package internal

import (
	"bytes"
	"encoding/json/v2"
	"io"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/oras-project/oras-go/v3"
	"github.com/oras-project/oras-go/v3/content"
	"github.com/oras-project/oras-go/v3/errdef"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/spec"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	ocitar "ocm.software/open-component-model/bindings/go/oci/tar"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// An access referrer holds accesses of the repository that wrote it, so no access referrer of the
// source may reach the target, however many the source lists and whatever they are stamped with:
// reads select the newest one, and a copied one could win over the one the target writes.
func TestTransferNormalizedComponentVersion_SourceAccessReferrersAreNotCopied(t *testing.T) {
	tests := []struct {
		name string
		// supersede adds another access referrer to the source.
		supersede func(t *testing.T, f *normalizedSource)
	}{
		{
			name: "source re-added once",
			supersede: func(t *testing.T, f *normalizedSource) {
				t.Helper()
				desc, err := f.source.repo.GetComponentVersion(t.Context(), normalizedComponent, normalizedVersion)
				require.NoError(t, err)
				require.NoError(t, f.source.repo.AddComponentVersion(t.Context(), desc))
			},
		},
		{
			// A writer whose clock is ahead: on the target, its access referrer would be
			// selected over every access referrer the target writes for the next hour.
			name: "source access referrer created in the future",
			supersede: func(t *testing.T, f *normalizedSource) {
				t.Helper()
				store, _ := f.source.store(t, normalizedComponent, normalizedVersion)
				var manifest ociImageSpecV1.Manifest
				require.NoError(t, json.Unmarshal(fetchAll(t, store, f.accessReferrers(t, f.source)[0]), &manifest))
				manifest.Annotations[ociImageSpecV1.AnnotationCreated] = time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
				pushJSON(t, store, manifest.MediaType, manifest)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			f := newNormalizedSource(t)
			target := f.provider.add(t, "/target", oci.WithComponentVersionLayout(oci.LayoutNormalized))
			tt.supersede(t, f)
			r.Len(f.accessReferrers(t, f.source), 2)

			r.NoError(f.transfer(t, target))

			f.requireCopied(t, target, f.signatures)
			targetAccess := f.requireOwnAccessReferrers(t, target, 1)
			created, err := time.Parse(time.RFC3339Nano, targetAccess[0].Annotations[ociImageSpecV1.AnnotationCreated])
			r.NoError(err)
			r.False(created.After(time.Now()), "the access referrer of the target is stamped by the target")
			// The only access referrer there is to select is the one the target wrote.
			_, err = target.repo.GetComponentVersion(t.Context(), normalizedComponent, normalizedVersion)
			r.NoError(err)
		})
	}
}

// With an OCI-native local blob the access referrer is an image index, which lists the manifest
// of the local blob. It is no more copied than an access manifest: the target writes its own.
func TestTransferNormalizedComponentVersion_SourceAccessIndexIsNotCopied(t *testing.T) {
	const imageResource = "image"
	r := require.New(t)
	ctx := t.Context()

	f := newNormalizedSourceWith(t, func(t *testing.T, source *ctfRepo, desc *descriptor.Descriptor) {
		t.Helper()
		ociLayout := singleLayerOCILayout(t, []byte("layer of an OCI-native local blob"))
		resource := descriptor.Resource{
			ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: imageResource, Version: normalizedVersion}},
			Type:        "ociArtifact",
			Relation:    descriptor.LocalRelation,
			Access: &descriptorv2.LocalBlob{
				Type:           runtime.NewVersionedType(descriptorv2.LocalBlobAccessType, descriptorv2.LocalBlobAccessTypeVersion),
				LocalReference: digest.FromBytes(ociLayout).String(),
				MediaType:      layout.MediaTypeOCIImageLayoutTarV1,
			},
		}
		updated, err := source.repo.AddLocalResource(t.Context(), normalizedComponent, normalizedVersion, &resource, inmemory.New(bytes.NewReader(ociLayout)))
		require.NoError(t, err)
		desc.Component.Resources = append(desc.Component.Resources, *updated)
	})
	target := f.provider.add(t, "/target", oci.WithComponentVersionLayout(oci.LayoutNormalized))
	sourceAccess := f.accessReferrers(t, f.source)
	r.Len(sourceAccess, 1)
	r.Equal(ociImageSpecV1.MediaTypeImageIndex, sourceAccess[0].MediaType, "the source must store its access referrer as index")

	r.NoError(f.transfer(t, target))

	f.requireCopied(t, target, f.signatures)
	targetAccess := f.requireOwnAccessReferrers(t, target, 1)
	r.Equal(ociImageSpecV1.MediaTypeImageIndex, targetAccess[0].MediaType, "the target must write its own access index")

	got, err := target.repo.GetComponentVersion(ctx, normalizedComponent, normalizedVersion)
	r.NoError(err)
	r.Len(got.Component.Resources, 2)
	for _, name := range []string{normalizedResource, imageResource} {
		localBlob, _, err := target.repo.GetLocalResource(ctx, normalizedComponent, normalizedVersion, runtime.Identity{"name": name})
		r.NoError(err, "local blob %s must be readable from the access referrer of the target", name)
		reader, err := localBlob.ReadCloser()
		r.NoError(err)
		_, err = io.Copy(io.Discard, reader)
		r.NoError(err)
		r.NoError(reader.Close())
	}
}

// A referrers listing is not bound to the manifests it lists: a referrers tag schema index is
// maintained by clients and can report another artifact type than the manifest has. The
// signature view of the source cannot tell such an access referrer from a signature, so the
// target has to reject it by its content, before the version is tagged.
func TestTransferNormalizedComponentVersion_MislabeledSourceAccessReferrerFailsTheCopy(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	f := newNormalizedSource(t)
	target := f.provider.add(t, "/target", oci.WithComponentVersionLayout(oci.LayoutNormalized))

	// The source is re-added, and its listing then reports the superseded access referrer as a
	// cosign signature. Reads of the source still select the access referrer listed as such.
	superseded := f.accessReferrers(t, f.source)[0]
	desc, err := f.source.repo.GetComponentVersion(ctx, normalizedComponent, normalizedVersion)
	r.NoError(err)
	r.NoError(f.source.repo.AddComponentVersion(ctx, desc))
	sourceStore, _ := f.source.store(t, normalizedComponent, normalizedVersion)
	relabelReferrer(t, sourceStore, f.normalized, superseded, cosignSignatureArtifactType)
	r.Len(f.accessReferrers(t, f.source), 1)
	r.Contains(digestsOf(f.source.referrers(t, normalizedComponent, normalizedVersion, f.normalized, cosignSignatureArtifactType)), superseded.Digest)

	err = f.transfer(t, target)
	r.ErrorContains(err, "access referrer "+superseded.Digest.String()+" must not be copied")

	targetStore, targetReference := target.store(t, normalizedComponent, normalizedVersion)
	_, err = targetStore.Resolve(ctx, targetReference)
	r.ErrorIs(err, errdef.ErrNotFound, "the version must not be tagged")
	exists, err := targetStore.Exists(ctx, superseded)
	r.NoError(err)
	r.False(exists, "the access referrer of the source must not be copied")
	r.Empty(f.accessReferrers(t, target), "the target must not list an access referrer")
}

// relabelReferrer rewrites the referrers tag schema index of subject in store, so that it lists
// referrer with artifactType instead of the artifact type of its manifest.
func relabelReferrer(t *testing.T, store spec.Store, subject, referrer ociImageSpecV1.Descriptor, artifactType string) {
	t.Helper()
	r := require.New(t)
	ctx := t.Context()
	// https://github.com/opencontainers/distribution-spec/blob/v1.1.1/spec.md#referrers-tag-schema
	referrersTag := subject.Digest.Algorithm().String() + "-" + subject.Digest.Encoded()

	listed, err := store.Resolve(ctx, referrersTag)
	r.NoError(err)
	var index ociImageSpecV1.Index
	r.NoError(json.Unmarshal(fetchAll(t, store, listed), &index))
	relabeled := false
	for i := range index.Manifests {
		if index.Manifests[i].Digest == referrer.Digest {
			index.Manifests[i].ArtifactType = artifactType
			relabeled = true
		}
	}
	r.True(relabeled, "referrer %s must be listed", referrer.Digest)
	r.NoError(store.Tag(ctx, pushJSON(t, store, index.MediaType, index), referrersTag))
}

// pushJSON pushes the manifest or index v to store and returns its descriptor.
func pushJSON(t *testing.T, store spec.Store, mediaType string, v any) ociImageSpecV1.Descriptor {
	t.Helper()
	raw, err := json.Marshal(v, runtime.JSONOptions)
	require.NoError(t, err)
	desc := content.NewDescriptorFromBytes(mediaType, raw)
	require.NoError(t, store.Push(t.Context(), desc, bytes.NewReader(raw)))
	return desc
}

// singleLayerOCILayout returns an OCI image layout tar that holds one manifest with data as its
// only layer.
func singleLayerOCILayout(t *testing.T, data []byte) []byte {
	t.Helper()
	r := require.New(t)
	ctx := t.Context()
	var buf bytes.Buffer
	w, err := ocitar.NewOCILayoutWriterWithTempFile(&buf, t.TempDir())
	r.NoError(err)

	layer := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageLayer, data)
	r.NoError(w.Push(ctx, layer, bytes.NewReader(data)))
	manifest, err := oras.PackManifest(ctx, w, oras.PackManifestVersion1_1, "application/vnd.test.artifact", oras.PackManifestOptions{
		Layers: []ociImageSpecV1.Descriptor{layer},
	})
	r.NoError(err)
	r.NoError(w.Tag(ctx, manifest, "image:"+normalizedVersion))
	r.NoError(w.Close())
	return buf.Bytes()
}
