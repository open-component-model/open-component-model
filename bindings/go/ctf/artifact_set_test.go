package ctf_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ociimagespecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/ctf"
)

const artifactSetMediaType = "application/vnd.oci.image.manifest.v1" + ctf.SynthesizedBlobFormatSuffix

func TestNewArtifactSetFromBlob_Compression(t *testing.T) {
	r := require.New(t)
	compressed, fixture := artifactSetArchive(t, ctf.ArtifactSetFormatLegacyOCI)
	gz, err := gzip.NewReader(bytes.NewReader(compressed))
	r.NoError(err)
	uncompressed, err := io.ReadAll(gz)
	r.NoError(err)

	for name, archive := range map[string][]byte{
		// smaller than a tar header, so the gzip sniffing must not pad it
		"gzip smaller than 512 bytes": compressed,
		"uncompressed tar":            uncompressed,
	} {
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			if name == "gzip smaller than 512 bytes" {
				r.Less(len(archive), 512)
			}
			as, err := ctf.NewArtifactSetFromBlob(inmemory.New(bytes.NewReader(archive), inmemory.WithMediaType(artifactSetMediaType)))
			r.NoError(err)
			t.Cleanup(func() { r.NoError(as.Close()) })

			r.Equal(ctf.ArtifactSetFormatLegacyOCI, as.Format())
			blobs, err := as.ListBlobs(t.Context())
			r.NoError(err)
			r.ElementsMatch([]string{fixture.manifest.Digest.String(), fixture.config.Digest.String(), fixture.layer.Digest.String()}, blobs)
		})
	}
}

func TestIsArtifactSetMediaType(t *testing.T) {
	for mt, want := range map[string]bool{
		"application/vnd.oci.image.manifest.v1" + ctf.SynthesizedBlobFormatSuffix:                true,
		"application/vnd.oci.image.index.v1" + ctf.SynthesizedBlobFormatSuffix:                   true,
		"application/vnd.docker.distribution.manifest.v2" + ctf.SynthesizedBlobFormatSuffix:      true,
		"application/vnd.docker.distribution.manifest.list.v2" + ctf.SynthesizedBlobFormatSuffix: true,
		"application/vnd.docker.distribution.manifest.v2+json":                                   false,
		"application/vnd.oci.image.manifest.v1+json":                                             false,
	} {
		t.Run(mt, func(t *testing.T) {
			require.Equal(t, want, ctf.IsArtifactSetMediaType(mt))
		})
	}
}

func TestNewArtifactSetFromBlob_IncompleteIndexEntry(t *testing.T) {
	r := require.New(t)
	manifestData := []byte(`{"schemaVersion":2,"config":{},"layers":[]}`)
	dig := digest.FromBytes(manifestData)

	for _, tc := range []struct {
		name    string
		entry   ociimagespecv1.Descriptor
		wantErr string
	}{
		{
			name:    "without media type",
			entry:   ociimagespecv1.Descriptor{Digest: dig, Size: int64(len(manifestData))},
			wantErr: "artifact set index entry " + dig.String() + " has no media type",
		},
		{
			name:    "without size",
			entry:   ociimagespecv1.Descriptor{MediaType: ociimagespecv1.MediaTypeImageManifest, Digest: dig},
			wantErr: "artifact set index entry " + dig.String() + " has no size",
		},
	} {
		indexData, err := json.Marshal(ociimagespecv1.Index{
			Versioned: specs.Versioned{SchemaVersion: 2},
			Manifests: []ociimagespecv1.Descriptor{tc.entry},
		})
		r.NoError(err)

		for _, format := range []ctf.ArtifactSetFormat{
			ctf.ArtifactSetFormatLegacyOCI,
			ctf.ArtifactSetFormatOCM,
			ctf.ArtifactSetFormatOCI,
		} {
			t.Run(tc.name+"/"+format.String(), func(t *testing.T) {
				r := require.New(t)
				archive := writeArtifactSet(t, format, indexData, map[string][]byte{dig.String(): manifestData})
				_, err := ctf.NewArtifactSetFromBlob(inmemory.New(bytes.NewReader(archive), inmemory.WithMediaType(artifactSetMediaType)))
				r.ErrorContains(err, tc.wantErr)
			})
		}
	}
}

// oversizedBlob declares size bytes but its reader yields more, like a source
// whose stream continues past the blob.
type oversizedBlob struct {
	data []byte
	size int64
}

func (b *oversizedBlob) ReadCloser() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(b.data)), nil
}
func (b *oversizedBlob) Size() int64               { return b.size }
func (b *oversizedBlob) MediaType() (string, bool) { return artifactSetMediaType, true }

func TestNewArtifactSetFromBlob_LimitsToBlobSize(t *testing.T) {
	r := require.New(t)
	archive, _ := artifactSetArchive(t, ctf.ArtifactSetFormatLegacyOCI)

	as, err := ctf.NewArtifactSetFromBlob(&oversizedBlob{
		data: append(bytes.Clone(archive), []byte("trailing bytes beyond the blob")...),
		size: int64(len(archive)),
	})
	r.NoError(err)
	r.NoError(as.Close())
}

type artifactSetFixture struct {
	manifest, config, layer ociimagespecv1.Descriptor
	data                    map[string][]byte
}

// artifactSetArchive builds a gzipped artifact set holding a single image
// manifest tagged v1.0.0, laid out according to format.
func artifactSetArchive(t *testing.T, format ctf.ArtifactSetFormat) ([]byte, artifactSetFixture) {
	t.Helper()
	r := require.New(t)

	layerData := []byte("layer")
	layer := content.NewDescriptorFromBytes(ociimagespecv1.MediaTypeImageLayer, layerData)
	configData := []byte(`{}`)
	config := content.NewDescriptorFromBytes(ociimagespecv1.MediaTypeImageConfig, configData)
	manifestData, err := json.Marshal(ociimagespecv1.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ociimagespecv1.MediaTypeImageManifest,
		Config:    config,
		Layers:    []ociimagespecv1.Descriptor{layer},
	})
	r.NoError(err)
	manifest := content.NewDescriptorFromBytes(ociimagespecv1.MediaTypeImageManifest, manifestData)

	tagged := manifest
	tagged.Annotations = map[string]string{ociimagespecv1.AnnotationRefName: "v1.0.0"}
	indexData, err := json.Marshal(ociimagespecv1.Index{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ociimagespecv1.MediaTypeImageIndex,
		Manifests: []ociimagespecv1.Descriptor{tagged},
	})
	r.NoError(err)

	data := map[string][]byte{}
	for _, entry := range []struct {
		desc ociimagespecv1.Descriptor
		data []byte
	}{{layer, layerData}, {config, configData}, {manifest, manifestData}} {
		data[entry.desc.Digest.String()] = entry.data
	}

	return writeArtifactSet(t, format, indexData, data), artifactSetFixture{manifest: manifest, config: config, layer: layer, data: data}
}

// writeArtifactSet writes a gzipped artifact set with the given index and blobs
// (keyed by digest), laid out according to format.
func writeArtifactSet(t *testing.T, format ctf.ArtifactSetFormat, indexData []byte, blobs map[string][]byte) []byte {
	t.Helper()
	r := require.New(t)

	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	write := func(name string, data []byte) {
		r.NoError(tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(data))}))
		_, err := tw.Write(data)
		r.NoError(err)
	}

	switch format {
	case ctf.ArtifactSetFormatOCM:
		write(ctf.ArtifactSetDescriptorFileName, indexData)
	case ctf.ArtifactSetFormatOCI:
		write(ociimagespecv1.ImageLayoutFile, []byte(`{"imageLayoutVersion":"1.0.0"}`))
		write(ociimagespecv1.ImageIndexFile, indexData)
	default:
		write(ociimagespecv1.ImageIndexFile, indexData)
	}

	for dig, data := range blobs {
		if format == ctf.ArtifactSetFormatOCI {
			d := digest.Digest(dig)
			write("blobs/"+d.Algorithm().String()+"/"+d.Encoded(), data)
			continue
		}
		name, err := ctf.ToBlobFileName(dig)
		r.NoError(err)
		write("blobs/"+name, data)
	}
	r.NoError(tw.Close())
	r.NoError(gz.Close())

	return archive.Bytes()
}
