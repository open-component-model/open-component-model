package tar

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content"
)

// BenchmarkLayoutVerifier_LayerSize verifies an uncompressed layout holding one
// layer of growing size. B/op stays flat for every shape: binary layers fail the
// JSON decoder at their first byte, and JSON layers are decoded as a stream with
// every token truncated to jsonTokenKeep.
func BenchmarkLayoutVerifier_LayerSize(b *testing.B) {
	for _, shape := range []struct {
		name string
		data func(size int) []byte
	}{
		{"binary", func(size int) []byte { return bytes.Repeat([]byte{'x'}, size) }},
		{"invalid-json", func(size int) []byte {
			data := bytes.Repeat([]byte{'a'}, size)
			data[0] = '{'
			return data
		}},
		{"valid-json", func(size int) []byte {
			return []byte(`{"x":[` + strings.Repeat("1,", (size-9)/2) + `1]}`)
		}},
	} {
		for _, size := range []int{1 << 20, 16 << 20, 64 << 20, 256 << 20} {
			b.Run(fmt.Sprintf("%s/%dMiB", shape.name, size>>20), func(b *testing.B) {
				archive, root := layoutWithLayer(b, shape.data(size))
				b.SetBytes(int64(len(archive)))
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					rc := LayoutVerifier{}.Verify(b.Context(), io.NopCloser(bytes.NewReader(archive)), root, int64(len(archive)))
					if _, err := io.Copy(io.Discard, rc); err != nil {
						b.Fatal(err)
					}
					if err := rc.Close(); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

func layoutWithLayer(b *testing.B, layerData []byte) ([]byte, digest.Digest) {
	b.Helper()
	r := require.New(b)
	layer := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageLayer, layerData)
	configData := []byte(`{}`)
	config := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageConfig, configData)
	manifestData, err := json.Marshal(ociImageSpecV1.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Config:    config,
		Layers:    []ociImageSpecV1.Descriptor{layer},
	})
	r.NoError(err)
	manifest := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageManifest, manifestData)
	indexData, err := json.Marshal(ociImageSpecV1.Index{
		Versioned: specs.Versioned{SchemaVersion: 2},
		Manifests: []ociImageSpecV1.Descriptor{manifest},
	})
	r.NoError(err)

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range []struct {
		name string
		data []byte
	}{
		{"blobs/sha256/" + layer.Digest.Encoded(), layerData},
		{"blobs/sha256/" + config.Digest.Encoded(), configData},
		{"blobs/sha256/" + manifest.Digest.Encoded(), manifestData},
		{ociImageSpecV1.ImageIndexFile, indexData},
	} {
		r.NoError(tw.WriteHeader(&tar.Header{Name: e.name, Mode: 0o644, Size: int64(len(e.data)), Typeflag: tar.TypeReg}))
		_, err := tw.Write(e.data)
		r.NoError(err)
	}
	r.NoError(tw.Close())
	return buf.Bytes(), manifest.Digest
}
