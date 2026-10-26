package ctf_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ociimagespecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/oci"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/ctf"
)

// TestNewArtifactSetFromBlob_IncompleteIndexDescriptors covers index entries
// written by the legacy OCM library without mediaType or size. Without them,
// oras cannot walk the graph below the entry.
func TestNewArtifactSetFromBlob_IncompleteIndexDescriptors(t *testing.T) {
	const dockerManifest = "application/vnd.docker.distribution.manifest.v2+json"

	layerData := []byte("layer")
	layer := content.NewDescriptorFromBytes(ociimagespecv1.MediaTypeImageLayer, layerData)
	configData := []byte(`{}`)
	config := content.NewDescriptorFromBytes(ociimagespecv1.MediaTypeImageConfig, configData)
	manifestJSON := func(mediaType string) []byte {
		data, err := json.Marshal(ociimagespecv1.Manifest{
			Versioned: specs.Versioned{SchemaVersion: 2},
			MediaType: mediaType,
			Config:    config,
			Layers:    []ociimagespecv1.Descriptor{layer},
		})
		require.NoError(t, err)
		return data
	}
	manifestData := manifestJSON(ociimagespecv1.MediaTypeImageManifest)
	manifest := content.NewDescriptorFromBytes(ociimagespecv1.MediaTypeImageManifest, manifestData)
	indexData, err := json.Marshal(ociimagespecv1.Index{
		Versioned: specs.Versioned{SchemaVersion: 2},
		Manifests: []ociimagespecv1.Descriptor{manifest},
	})
	require.NoError(t, err)

	for _, tc := range []struct {
		name          string
		root          []byte
		wantMediaType string
		// child is a descriptor below root whose predecessor must be root
		child ociimagespecv1.Descriptor
	}{
		{
			name:          "manifest declaring its media type",
			root:          manifestData,
			wantMediaType: ociimagespecv1.MediaTypeImageManifest,
			child:         layer,
		},
		{
			name:          "manifest without media type",
			root:          manifestJSON(""),
			wantMediaType: ociimagespecv1.MediaTypeImageManifest,
			child:         layer,
		},
		{
			name:          "docker manifest keeps its media type",
			root:          manifestJSON(dockerManifest),
			wantMediaType: dockerManifest,
			child:         layer,
		},
		{
			name:          "index without media type",
			root:          indexData,
			wantMediaType: ociimagespecv1.MediaTypeImageIndex,
			child:         manifest,
		},
	} {
		for _, format := range []ctf.ArtifactSetFormat{
			ctf.ArtifactSetFormatLegacyOCI,
			ctf.ArtifactSetFormatOCM,
			ctf.ArtifactSetFormatOCI,
		} {
			t.Run(tc.name+"/"+format.String(), func(t *testing.T) {
				r := require.New(t)
				ctx := t.Context()

				rootDigest := digest.FromBytes(tc.root)
				as := openArtifactSet(t, format, ociimagespecv1.Descriptor{
					Digest:      rootDigest,
					Annotations: map[string]string{ociimagespecv1.AnnotationRefName: "v1.0.0"},
				}, map[string][]byte{
					rootDigest.String():      tc.root,
					manifest.Digest.String(): manifestData,
					config.Digest.String():   configData,
					layer.Digest.String():    layerData,
				})

				idx := as.GetIndex()
				r.Len(idx.Manifests, 1)
				r.Equal(tc.wantMediaType, idx.Manifests[0].MediaType)
				r.Equal(int64(len(tc.root)), idx.Manifests[0].Size)
				r.Equal("v1.0.0", idx.Manifests[0].Annotations[ociimagespecv1.AnnotationRefName])

				store, err := oci.NewFromFS(ctx, as.OCILayoutFS())
				r.NoError(err)
				root, err := store.Resolve(ctx, "v1.0.0")
				r.NoError(err)
				r.Equal(rootDigest, root.Digest)
				predecessors, err := store.Predecessors(ctx, tc.child)
				r.NoError(err)
				r.Contains(digests(predecessors), rootDigest)
			})
		}
	}

	t.Run("undeterminable media type fails", func(t *testing.T) {
		data := []byte(`{"schemaVersion":2}`)
		archive := writeArtifactSet(t, ctf.ArtifactSetFormatLegacyOCI,
			indexJSON(t, ociimagespecv1.Descriptor{Digest: digest.FromBytes(data)}),
			map[string][]byte{digest.FromBytes(data).String(): data})
		_, err := ctf.NewArtifactSetFromBlob(inmemory.New(bytes.NewReader(archive), inmemory.WithMediaType(artifactSetMediaType)))
		require.ErrorContains(t, err, "unable to determine media type")
	})

	t.Run("missing blob fails", func(t *testing.T) {
		archive := writeArtifactSet(t, ctf.ArtifactSetFormatLegacyOCI,
			indexJSON(t, ociimagespecv1.Descriptor{Digest: manifest.Digest}), nil)
		_, err := ctf.NewArtifactSetFromBlob(inmemory.New(bytes.NewReader(archive), inmemory.WithMediaType(artifactSetMediaType)))
		require.ErrorContains(t, err, manifest.Digest.Encoded())
	})
}

func openArtifactSet(t *testing.T, format ctf.ArtifactSetFormat, root ociimagespecv1.Descriptor, blobs map[string][]byte) *ctf.ArtifactSet {
	t.Helper()
	r := require.New(t)
	archive := writeArtifactSet(t, format, indexJSON(t, root), blobs)
	as, err := ctf.NewArtifactSetFromBlob(inmemory.New(bytes.NewReader(archive), inmemory.WithMediaType(artifactSetMediaType)))
	r.NoError(err)
	t.Cleanup(func() { r.NoError(as.Close()) })
	r.Equal(format, as.Format())
	return as
}

func indexJSON(t *testing.T, manifests ...ociimagespecv1.Descriptor) []byte {
	t.Helper()
	data, err := json.Marshal(ociimagespecv1.Index{
		Versioned: specs.Versioned{SchemaVersion: 2},
		Manifests: manifests,
	})
	require.NoError(t, err)
	return data
}

func digests(descs []ociimagespecv1.Descriptor) []digest.Digest {
	out := make([]digest.Digest, 0, len(descs))
	for _, d := range descs {
		out = append(out, d.Digest)
	}
	return out
}
