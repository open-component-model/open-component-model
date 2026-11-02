package tar_test

import (
	archivetar "archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"testing"

	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/ctf"
	"ocm.software/open-component-model/bindings/go/oci/tar"
)

// Customer local blobs arrive with docker (not OCI) artifact-set media types, e.g.
// application/vnd.docker.distribution.manifest.v2+tar+gzip. Reading such a LegacyOCI
// artifact set through ReadOCILayout must preserve the main artifact digest and media
// type (no docker->OCI manifest conversion), so the uploaded blob stays byte-identical.
func Test_DockerArtifactSet_ReadOCILayout(t *testing.T) {
	const (
		dockerManifest    = "application/vnd.docker.distribution.manifest.v2+json"
		dockerConfig      = "application/vnd.docker.container.image.v1+json"
		dockerLayer       = "application/vnd.docker.image.rootfs.diff.tar.gzip"
		dockerArtifactSet = "application/vnd.docker.distribution.manifest.v2" + ctf.SynthesizedBlobFormatSuffix
	)

	r := require.New(t)

	layerData := []byte("layer")
	layer := content.NewDescriptorFromBytes(dockerLayer, layerData)
	configData := []byte(`{}`)
	config := content.NewDescriptorFromBytes(dockerConfig, configData)
	manifestData, err := json.Marshal(ociImageSpecV1.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: dockerManifest,
		Config:    config,
		Layers:    []ociImageSpecV1.Descriptor{layer},
	})
	r.NoError(err)
	manifest := content.NewDescriptorFromBytes(dockerManifest, manifestData)

	tagged := manifest
	tagged.Annotations = map[string]string{ociImageSpecV1.AnnotationRefName: "v1.0.0"}
	indexData, err := json.Marshal(ociImageSpecV1.Index{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ociImageSpecV1.MediaTypeImageIndex,
		Manifests: []ociImageSpecV1.Descriptor{tagged},
	})
	r.NoError(err)

	// LegacyOCI layout: oci-layout + index.json + flat blobs/<algo>.<encoded>.
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := archivetar.NewWriter(gz)
	write := func(name string, data []byte) {
		r.NoError(tw.WriteHeader(&archivetar.Header{Name: name, Mode: 0o644, Size: int64(len(data))}))
		_, err := tw.Write(data)
		r.NoError(err)
	}
	write(ociImageSpecV1.ImageLayoutFile, []byte(`{"imageLayoutVersion":"1.0.0"}`))
	write(ociImageSpecV1.ImageIndexFile, indexData)
	for _, e := range []struct {
		desc ociImageSpecV1.Descriptor
		data []byte
	}{{layer, layerData}, {config, configData}, {manifest, manifestData}} {
		name, err := ctf.ToBlobFileName(e.desc.Digest.String())
		r.NoError(err)
		write("blobs/"+name, e.data)
	}
	r.NoError(tw.Close())
	r.NoError(gz.Close())

	r.True(ctf.IsArtifactSetMediaType(dockerArtifactSet),
		"docker artifact-set media type must be recognized")

	b := inmemory.New(bytes.NewReader(archive.Bytes()), inmemory.WithMediaType(dockerArtifactSet))
	store, err := tar.ReadOCILayout(t.Context(), b)
	r.NoError(err)
	t.Cleanup(func() { r.NoError(store.Close()) })

	main := store.MainArtifacts(t.Context())
	r.Len(main, 1)
	r.Equal(manifest.Digest, main[0].Digest, "main artifact digest must not change")
	r.Equal(dockerManifest, main[0].MediaType, "docker media type must be preserved")
}
