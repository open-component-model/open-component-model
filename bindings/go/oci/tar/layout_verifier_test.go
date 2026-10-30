package tar

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
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

func blobEntry(desc ociImageSpecV1.Descriptor, data []byte) tarEntry {
	return tarEntry{name: "blobs/" + desc.Digest.Algorithm().String() + "/" + desc.Digest.Encoded(), data: data}
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

func TestLayoutVerifier_WarnsAboutUntrackedContent(t *testing.T) {
	var logged bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	configData := []byte(`{}`)
	config := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageConfig, configData)
	manifestWith := func(layer ociImageSpecV1.Descriptor) (ociImageSpecV1.Descriptor, []byte) {
		data, err := json.Marshal(ociImageSpecV1.Manifest{
			Versioned: specs.Versioned{SchemaVersion: 2},
			MediaType: ociImageSpecV1.MediaTypeImageManifest,
			Config:    config,
			Layers:    []ociImageSpecV1.Descriptor{layer},
		})
		require.NoError(t, err)
		return content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageManifest, data), data
	}
	layerData, otherLayerData, strayData := []byte("layer"), []byte("other layer"), []byte("stray")
	layer := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageLayer, layerData)
	otherLayer := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageLayer, otherLayerData)
	stray := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageLayer, strayData)
	main, mainData := manifestWith(layer)
	other, otherData := manifestWith(otherLayer)
	main.Annotations = map[string]string{ociImageSpecV1.AnnotationRefName: "main"}
	indexData, err := json.Marshal(ociImageSpecV1.Index{Versioned: specs.Versioned{SchemaVersion: 2}, Manifests: []ociImageSpecV1.Descriptor{main, other}})
	require.NoError(t, err)
	tracked := []tarEntry{
		{name: ociImageSpecV1.ImageLayoutFile, data: []byte(`{"imageLayoutVersion":"1.0.0"}`)},
		blobEntry(config, configData), blobEntry(layer, layerData), blobEntry(main, mainData),
		blobEntry(otherLayer, otherLayerData), blobEntry(other, otherData),
		{name: ociImageSpecV1.ImageIndexFile, data: indexData},
	}

	for _, tc := range []struct {
		name  string
		extra []tarEntry
		want  []string
	}{
		{name: "everything reachable from the index"},
		{name: "a blob nothing references", extra: []tarEntry{blobEntry(stray, strayData)}, want: []string{"unreferencedBlobs=1", stray.Digest.String()}},
		{name: "a file outside the layout structure", extra: []tarEntry{{name: "notes.txt", data: []byte("hi")}}, want: []string{"otherFiles=1", "notes.txt"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			logged.Reset()
			r.NoError(verifyLayout(gzippedTar(t, append(slices.Clone(tracked), tc.extra...)...), main.Digest))
			if len(tc.want) == 0 {
				r.Empty(logged.String())
				return
			}
			r.Contains(logged.String(), "layout holds content that neither its index nor its main artifact references")
			for _, want := range tc.want {
				r.Contains(logged.String(), want)
			}
		})
	}
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

	t.Run("a listed manifest that is not one", func(t *testing.T) {
		r := require.New(t)
		notManifestData := []byte("not a manifest")
		notManifest := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageManifest, notManifestData)
		archive := gzippedTar(t,
			blobEntry(config, configData), blobEntry(layer, layerData),
			blobEntry(root, rootData), blobEntry(notManifest, notManifestData),
			indexOf(named, notManifest),
		)
		r.ErrorContains(verifyLayout(archive, root.Digest),
			"layout manifest "+notManifest.Digest.String()+" listed in the index cannot be inspected")
	})
}

func TestLayoutVerifier_LargeManifests(t *testing.T) {
	layerData := []byte("layer")
	layer := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageLayer, layerData)
	configData := []byte(`{}`)
	config := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageConfig, configData)
	missingLayer := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageLayer, []byte("missing"))
	manifestWith := func(annotations map[string]string) []byte {
		data, err := json.Marshal(ociImageSpecV1.Manifest{
			Versioned:   specs.Versioned{SchemaVersion: 2},
			MediaType:   ociImageSpecV1.MediaTypeImageManifest,
			Config:      config,
			Layers:      []ociImageSpecV1.Descriptor{layer},
			Annotations: annotations,
		})
		require.NoError(t, err)
		return data
	}
	manyMembers := func(total int) map[string]string {
		annotations := make(map[string]string, total/64)
		for i := range total / 64 {
			annotations[fmt.Sprintf("k%d", i)] = strings.Repeat("a", 64)
		}
		return annotations
	}
	missingJSON, err := json.Marshal(missingLayer)
	require.NoError(t, err)
	// withMember adds a member with raw as its value to the front of a manifest.
	withMember := func(name, raw string) []byte {
		return append([]byte(`{"`+name+`":`+raw+`,`), manifestWith(nil)[1:]...)
	}
	long := 5 << 20

	for _, tc := range []struct {
		name     string
		manifest func() []byte
		wantErr  string
	}{
		{
			name: "above the former 4 MiB cap, with leading whitespace",
			manifest: func() []byte {
				return append([]byte("\n  "), manifestWith(manyMembers(long))...)
			},
		},
		{
			name: "a long string with escapes",
			manifest: func() []byte {
				// encoding/json writes \" \\ and \u003c escapes for these.
				return manifestWith(map[string]string{"pad": strings.Repeat(`a"\<é`, long/5)})
			},
		},
		{
			name: "a long member name",
			manifest: func() []byte {
				return withMember(strings.Repeat("n", long), "1")
			},
		},
		{
			name: "a long number",
			manifest: func() []byte {
				return withMember("n", "-1"+strings.Repeat("0", long)+".5"+strings.Repeat("5", long)+"e+1"+strings.Repeat("0", long))
			},
		},
		{
			name: "a long whitespace run",
			manifest: func() []byte {
				return withMember("n", strings.Repeat(" \n", long)+"1")
			},
		},
		{
			// encoding/json matches names case-insensitively, so consumers see these layers.
			name: "layers under a differently cased name",
			manifest: func() []byte {
				var m map[string]any
				require.NoError(t, json.Unmarshal(manifestWith(nil), &m))
				m["Layers"] = []json.RawMessage{missingJSON}
				delete(m, "layers")
				data, err := json.Marshal(m)
				require.NoError(t, err)
				return data
			},
			wantErr: "layout blob " + missingLayer.Digest.String() + " referenced by the artifact graph is missing",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			manifestData := tc.manifest()
			manifest := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageManifest, manifestData)
			indexData, err := json.Marshal(ociImageSpecV1.Index{Versioned: specs.Versioned{SchemaVersion: 2}, Manifests: []ociImageSpecV1.Descriptor{manifest}})
			r.NoError(err)
			archive := gzippedTar(t,
				blobEntry(layer, layerData), blobEntry(config, configData), blobEntry(manifest, manifestData),
				tarEntry{name: ociImageSpecV1.ImageIndexFile, data: indexData},
			)
			err = verifyLayout(archive, manifest.Digest)
			if tc.wantErr == "" {
				r.NoError(err)
				return
			}
			r.ErrorContains(err, tc.wantErr)
		})
	}
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
