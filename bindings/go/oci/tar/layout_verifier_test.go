package tar

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/memory"

	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
)

type tarEntry struct {
	name string
	data []byte
}

func gzippedTar(t *testing.T, entries ...tarEntry) []byte {
	t.Helper()
	r := require.New(t)
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		r.NoError(tw.WriteHeader(&tar.Header{Name: e.name, Mode: 0o644, Size: int64(len(e.data)), Typeflag: tar.TypeReg}))
		_, err := tw.Write(e.data)
		r.NoError(err)
	}
	r.NoError(tw.Close())
	r.NoError(gz.Close())
	return buf.Bytes()
}

// verifyLayout reads archive through a LayoutVerifier like a consumer copying it would.
func verifyLayout(archive []byte, root digest.Digest) error {
	rc := LayoutVerifier{}.Verify(io.NopCloser(bytes.NewReader(archive)), root, -1)
	_, err := io.ReadAll(rc)
	return errors.Join(err, rc.Close())
}

func readAllBlob(t *testing.T, b interface {
	ReadCloser() (io.ReadCloser, error)
},
) []byte {
	t.Helper()
	rc, err := b.ReadCloser()
	require.NoError(t, err)
	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.NoError(t, rc.Close())
	return data
}

func pushBytes(t *testing.T, store *memory.Store, mediaType string, data []byte) ociImageSpecV1.Descriptor {
	t.Helper()
	desc := content.NewDescriptorFromBytes(mediaType, data)
	require.NoError(t, store.Push(t.Context(), desc, bytes.NewReader(data)))
	return desc
}

func TestLayoutVerifier_Accepts(t *testing.T) {
	r := require.New(t)
	for _, mt := range []string{
		layout.MediaTypeOCIImageLayoutTarGzipV1,
		layout.MediaTypeOCIImageLayoutTarV1,
		"application/vnd.oci.image.manifest.v1+tar+gzip",
		"application/vnd.oci.image.index.v1+tar+gzip",
	} {
		r.True(LayoutVerifier{}.Accepts(mt), mt)
	}
	r.False(LayoutVerifier{}.Accepts("application/octet-stream"))
	r.False(LayoutVerifier{}.Accepts(""))
}

func TestLayoutVerifier_OCMLayout(t *testing.T) {
	src := memory.New()
	manifestOf := func(layerData string) ociImageSpecV1.Descriptor {
		layer := pushBytes(t, src, "application/octet-stream", []byte(layerData))
		manifest, err := oras.PackManifest(t.Context(), src, oras.PackManifestVersion1_1, "application/artifact",
			oras.PackManifestOptions{Layers: []ociImageSpecV1.Descriptor{layer}})
		require.NoError(t, err)
		return manifest
	}

	single := manifestOf("single")
	indexData, err := json.Marshal(ociImageSpecV1.Index{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ociImageSpecV1.MediaTypeImageIndex,
		Manifests: []ociImageSpecV1.Descriptor{manifestOf("a"), manifestOf("b")},
	})
	require.NoError(t, err)
	index := pushBytes(t, src, ociImageSpecV1.MediaTypeImageIndex, indexData)

	for name, root := range map[string]ociImageSpecV1.Descriptor{"manifest": single, "index": index} {
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			b, err := CopyToOCILayoutInMemory(t.Context(), src, root, CopyToOCILayoutOptions{Tags: []string{"v1"}, TempDir: t.TempDir()})
			r.NoError(err)
			archive := readAllBlob(t, b)

			r.NoError(verifyLayout(archive, root.Digest))
			r.ErrorContains(verifyLayout(archive, digest.FromString("other")), "layout main artifact is")
		})
	}
}

func TestLayoutVerifier_RefusesTamperedLayouts(t *testing.T) {
	layerData := []byte("layer")
	layer := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageLayer, layerData)
	configData := []byte(`{}`)
	config := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageConfig, configData)
	manifestData, err := json.Marshal(ociImageSpecV1.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Config:    config,
		Layers:    []ociImageSpecV1.Descriptor{layer},
	})
	require.NoError(t, err)
	manifest := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageManifest, manifestData)

	index := func(annotations map[string]string) tarEntry {
		data, err := json.Marshal(ociImageSpecV1.Index{
			Versioned:   specs.Versioned{SchemaVersion: 2},
			Manifests:   []ociImageSpecV1.Descriptor{manifest},
			Annotations: annotations,
		})
		require.NoError(t, err)
		return tarEntry{name: ociImageSpecV1.ImageIndexFile, data: data}
	}
	blobEntry := func(desc ociImageSpecV1.Descriptor, data []byte) tarEntry {
		return tarEntry{name: "blobs/" + desc.Digest.Algorithm().String() + "/" + desc.Digest.Encoded(), data: data}
	}
	layoutFile := tarEntry{name: ociImageSpecV1.ImageLayoutFile, data: []byte(`{"imageLayoutVersion":"1.0.0"}`)}

	tests := []struct {
		name    string
		entries []tarEntry
		wantErr string
	}{
		{
			name:    "complete layout",
			entries: []tarEntry{layoutFile, blobEntry(config, configData), blobEntry(layer, layerData), blobEntry(manifest, manifestData), index(nil)},
		},
		{
			name:    "complete layout with a matching main annotation",
			entries: []tarEntry{index(map[string]string{annotationMainArtifact: manifest.Digest.String()}), blobEntry(config, configData), blobEntry(layer, layerData), blobEntry(manifest, manifestData)},
		},
		{
			name:    "tampered layer",
			entries: []tarEntry{layoutFile, blobEntry(config, configData), blobEntry(layer, []byte("tampered")), blobEntry(manifest, manifestData), index(nil)},
			wantErr: "does not match its digest",
		},
		{
			name:    "missing layer",
			entries: []tarEntry{layoutFile, blobEntry(config, configData), blobEntry(manifest, manifestData), index(nil)},
			wantErr: "referenced by the artifact graph is missing",
		},
		{
			name:    "missing manifest",
			entries: []tarEntry{layoutFile, blobEntry(config, configData), blobEntry(layer, layerData), index(nil)},
			wantErr: "listed in the index cannot be inspected",
		},
		{
			name:    "duplicate entry",
			entries: []tarEntry{layoutFile, blobEntry(config, configData), blobEntry(layer, layerData), blobEntry(layer, layerData), blobEntry(manifest, manifestData), index(nil)},
			wantErr: "more than once",
		},
		{
			name: "index.json alias following the index",
			entries: []tarEntry{
				layoutFile, blobEntry(config, configData), blobEntry(layer, layerData), blobEntry(manifest, manifestData), index(nil),
				{name: ".//" + ociImageSpecV1.ImageIndexFile, data: []byte(`{"schemaVersion":2,"manifests":[]}`)},
			},
			wantErr: "more than once",
		},
		{
			name: "blob alias following the blob",
			entries: []tarEntry{
				layoutFile, blobEntry(config, configData), blobEntry(layer, layerData), blobEntry(manifest, manifestData), index(nil),
				{name: ".//" + blobEntry(layer, nil).name, data: []byte("tampered")},
			},
			wantErr: "more than once",
		},
		{
			name: "absolute index.json alias following the index",
			entries: []tarEntry{
				layoutFile, blobEntry(config, configData), blobEntry(layer, layerData), blobEntry(manifest, manifestData), index(nil),
				{name: "/" + ociImageSpecV1.ImageIndexFile, data: []byte(`{"schemaVersion":2,"manifests":[]}`)},
			},
			wantErr: "more than once",
		},
		{
			name:    "main annotation naming another artifact",
			entries: []tarEntry{index(map[string]string{annotationMainArtifact: digest.FromString("other").String()}), blobEntry(config, configData), blobEntry(layer, layerData), blobEntry(manifest, manifestData)},
			wantErr: "layout annotation software.ocm/main names",
		},
		{
			name:    "no index",
			entries: []tarEntry{layoutFile, blobEntry(config, configData), blobEntry(layer, layerData), blobEntry(manifest, manifestData)},
			wantErr: "layout has no index.json",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			err := verifyLayout(gzippedTar(t, tt.entries...), manifest.Digest)
			if tt.wantErr == "" {
				r.NoError(err)
				return
			}
			r.ErrorContains(err, tt.wantErr)
		})
	}
}

func TestLayoutVerifier_BoundsRetainedManifests(t *testing.T) {
	r := require.New(t)
	// Every blob is manifest-shaped and just below maxLayoutManifestSize, so each is
	// retained individually, but together they exceed maxLayoutRetainedSize.
	pad := strings.Repeat("a", int(maxLayoutManifestSize)-64)
	count := int(maxLayoutRetainedSize/maxLayoutManifestSize) + 1
	entries := make([]tarEntry, 0, count)
	for i := range count {
		data := []byte(fmt.Sprintf(`{"schemaVersion":2,"n":%d,"pad":%q}`, i, pad))
		r.LessOrEqual(int64(len(data)), maxLayoutManifestSize)
		dig := digest.FromBytes(data)
		entries = append(entries, tarEntry{name: "blobs/" + dig.Algorithm().String() + "/" + dig.Encoded(), data: data})
	}
	r.ErrorContains(verifyLayout(gzippedTar(t, entries...), digest.FromString("root")), "layout manifests and indexes exceed")
}

func TestLayoutVerifier_LegacyArtifactSet(t *testing.T) {
	r := require.New(t)
	archive, err := os.ReadFile("../../ctf/testdata/compatibility/02/with-resource/blobs/sha256.e40e3a2f1ab1a98328dfd14539a79d27aff5c4d5c34cd16a85f0288bfa76490b")
	r.NoError(err)

	r.NoError(verifyLayout(archive, "sha256:62be4af3382a4493cb7f1dd4ec47bcb28f1863b615fc9e4a1dceefbe93898dd0"))
	r.ErrorContains(verifyLayout(archive, digest.FromString("other")), "layout main artifact is")

	built, manifest, _ := legacyArtifactSet(t)
	r.NoError(verifyLayout(built, manifest.Digest))
}

func TestLayoutVerifier_CloseCompletesVerification(t *testing.T) {
	src := memory.New()
	layer := pushBytes(t, src, "application/octet-stream", []byte("layer"))
	manifest, err := oras.PackManifest(t.Context(), src, oras.PackManifestVersion1_1, "application/artifact",
		oras.PackManifestOptions{Layers: []ociImageSpecV1.Descriptor{layer}})
	require.NoError(t, err)
	b, err := CopyToOCILayoutInMemory(t.Context(), src, manifest, CopyToOCILayoutOptions{TempDir: t.TempDir()})
	require.NoError(t, err)
	archive := readAllBlob(t, b)

	readPartially := func(root digest.Digest) error {
		rc := LayoutVerifier{}.Verify(io.NopCloser(bytes.NewReader(archive)), root, int64(len(archive)))
		_, err := io.ReadFull(rc, make([]byte, 10))
		require.NoError(t, err)
		return rc.Close()
	}

	r := require.New(t)
	r.NoError(readPartially(manifest.Digest))
	r.ErrorContains(readPartially(digest.FromString("other")), "layout main artifact is",
		"closing a partially read layout must still verify it")
}

func TestLayoutVerifier_RefusesRootsConsumersWouldNotPick(t *testing.T) {
	configData := []byte(`{}`)
	config := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageConfig, configData)
	layerData := []byte("layer")
	layer := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageLayer, layerData)
	manifestWith := func(layers ...ociImageSpecV1.Descriptor) (ociImageSpecV1.Descriptor, []byte) {
		data, err := json.Marshal(ociImageSpecV1.Manifest{
			Versioned: specs.Versioned{SchemaVersion: 2},
			MediaType: ociImageSpecV1.MediaTypeImageManifest,
			Config:    config,
			Layers:    layers,
		})
		require.NoError(t, err)
		return content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageManifest, data), data
	}
	blobEntry := func(desc ociImageSpecV1.Descriptor, data []byte) tarEntry {
		return tarEntry{name: "blobs/" + desc.Digest.Algorithm().String() + "/" + desc.Digest.Encoded(), data: data}
	}
	indexOf := func(manifests ...ociImageSpecV1.Descriptor) tarEntry {
		data, err := json.Marshal(ociImageSpecV1.Index{Versioned: specs.Versioned{SchemaVersion: 2}, Manifests: manifests})
		require.NoError(t, err)
		return tarEntry{name: ociImageSpecV1.ImageIndexFile, data: data}
	}

	root, rootData := manifestWith(layer)
	named := root
	named.Annotations = map[string]string{ociImageSpecV1.AnnotationRefName: "v1"}

	t.Run("an artifact holding the root as a layer", func(t *testing.T) {
		r := require.New(t)
		wrapper, wrapperData := manifestWith(root)
		archive := gzippedTar(t,
			blobEntry(config, configData), blobEntry(layer, layerData),
			blobEntry(root, rootData), blobEntry(wrapper, wrapperData),
			indexOf(named, wrapper),
		)
		r.ErrorContains(verifyLayout(archive, root.Digest), "layout main artifact is "+wrapper.Digest.String())
	})

	t.Run("a listed manifest too large to inspect", func(t *testing.T) {
		r := require.New(t)
		oversizedData := bytes.Repeat([]byte{'x'}, int(maxLayoutManifestSize)+1)
		oversized := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageManifest, oversizedData)
		archive := gzippedTar(t,
			blobEntry(config, configData), blobEntry(layer, layerData),
			blobEntry(root, rootData), blobEntry(oversized, oversizedData),
			indexOf(named, oversized),
		)
		r.ErrorContains(verifyLayout(archive, root.Digest),
			"layout manifest "+oversized.Digest.String()+" listed in the index cannot be inspected")
	})
}

func TestLayoutVerifier_VerifiesAtSize(t *testing.T) {
	src := memory.New()
	layer := pushBytes(t, src, "application/octet-stream", []byte("layer"))
	manifest, err := oras.PackManifest(t.Context(), src, oras.PackManifestVersion1_1, "application/artifact",
		oras.PackManifestOptions{Layers: []ociImageSpecV1.Descriptor{layer}})
	require.NoError(t, err)
	b, err := CopyToOCILayoutInMemory(t.Context(), src, manifest, CopyToOCILayoutOptions{TempDir: t.TempDir()})
	require.NoError(t, err)
	archive := readAllBlob(t, b)
	size := int64(len(archive))

	t.Run("a valid layout read to its size", func(t *testing.T) {
		r := require.New(t)
		rc := LayoutVerifier{}.Verify(io.NopCloser(bytes.NewReader(archive)), manifest.Digest, size)
		data, err := io.ReadAll(io.LimitReader(rc, size))
		r.NoError(err)
		r.Equal(archive, data)
		n, err := rc.Read(make([]byte, 1))
		r.Zero(n)
		r.ErrorIs(err, io.EOF)
		r.NoError(rc.Close())
	})

	t.Run("a wrong root fails the read reaching the size", func(t *testing.T) {
		r := require.New(t)
		rc := LayoutVerifier{}.Verify(io.NopCloser(bytes.NewReader(archive)), digest.FromString("other"), size)
		_, err := io.ReadAll(io.LimitReader(rc, size))
		r.ErrorContains(err, "layout main artifact is")
		r.Error(rc.Close())
	})

	t.Run("more bytes than the size", func(t *testing.T) {
		r := require.New(t)
		rc := LayoutVerifier{}.Verify(io.NopCloser(bytes.NewReader(archive)), manifest.Digest, size-1)
		_, err := io.ReadAll(rc)
		r.ErrorContains(err, "exceeds its size")
		r.Error(rc.Close())
	})
}
