package tar

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"testing"

	"github.com/opencontainers/image-spec/specs-go"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/ctf"
)

func TestReadOCILayout_ArtifactSet(t *testing.T) {
	r := require.New(t)
	archive, manifest, layer := legacyArtifactSet(t)
	r.NotEmpty(archive)

	t.Run("artifact set media type is read as OCI layout", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()

		store, err := ReadOCILayout(ctx, inmemory.New(bytes.NewReader(archive),
			inmemory.WithMediaType("application/vnd.oci.image.manifest.v1"+ctf.SynthesizedBlobFormatSuffix)))
		r.NoError(err)

		main := store.MainArtifacts(ctx)
		r.Len(main, 1)
		r.Equal(manifest.Digest, main[0].Digest)

		predecessors, err := store.Predecessors(ctx, layer)
		r.NoError(err)
		r.Len(predecessors, 1)
		r.Equal(manifest.Digest, predecessors[0].Digest)

		data, err := content.FetchAll(ctx, store, layer)
		r.NoError(err)
		r.Equal([]byte("layer"), data)

		r.NoError(store.Close())
	})

	t.Run("without artifact set media type it is not an OCI layout", func(t *testing.T) {
		r := require.New(t)
		_, err := ReadOCILayout(t.Context(), inmemory.New(bytes.NewReader(archive)))
		r.Error(err)
	})
}

// legacyArtifactSet builds a gzipped artifact set in ctf.ArtifactSetFormatLegacyOCI:
// index.json without oci-layout and blobs stored flat as blobs/<algorithm>.<encoded>.
func legacyArtifactSet(t *testing.T) (archive []byte, manifest, layer v1.Descriptor) {
	t.Helper()
	r := require.New(t)

	layerData := []byte("layer")
	layer = content.NewDescriptorFromBytes(v1.MediaTypeImageLayer, layerData)
	configData := []byte(`{}`)
	config := content.NewDescriptorFromBytes(v1.MediaTypeImageConfig, configData)
	manifestData, err := json.Marshal(v1.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: v1.MediaTypeImageManifest,
		Config:    config,
		Layers:    []v1.Descriptor{layer},
	})
	r.NoError(err)
	manifest = content.NewDescriptorFromBytes(v1.MediaTypeImageManifest, manifestData)

	tagged := manifest
	tagged.Annotations = map[string]string{v1.AnnotationRefName: "v1.0.0"}
	indexData, err := json.Marshal(v1.Index{
		Versioned: specs.Versioned{SchemaVersion: 2},
		Manifests: []v1.Descriptor{tagged},
	})
	r.NoError(err)

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	write := func(name string, data []byte) {
		r.NoError(tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(data))}))
		_, err := tw.Write(data)
		r.NoError(err)
	}
	write(v1.ImageIndexFile, indexData)
	for _, entry := range []struct {
		desc v1.Descriptor
		data []byte
	}{{layer, layerData}, {config, configData}, {manifest, manifestData}} {
		name, err := ctf.ToBlobFileName(entry.desc.Digest.String())
		r.NoError(err)
		write("blobs/"+name, entry.data)
	}
	r.NoError(tw.Close())
	r.NoError(gz.Close())

	return buf.Bytes(), manifest, layer
}
