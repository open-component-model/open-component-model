package tar_test

import (
	stdtar "archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"testing"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	ocitar "ocm.software/open-component-model/bindings/go/oci/tar"
)

// TestArtifactSetRoots covers both artifact-set descriptor names: index.json (OCI
// format) and artifact-descriptor.json (OCM-native format). Both are OCI image
// indexes, so the declared root must be found either way, with or without gzip.
func TestArtifactSetRoots(t *testing.T) {
	root := digest.FromString("the main artifact")
	index := ociImageSpecV1.Index{
		MediaType: ociImageSpecV1.MediaTypeImageIndex,
		Manifests: []ociImageSpecV1.Descriptor{{
			MediaType: ociImageSpecV1.MediaTypeImageManifest,
			Digest:    root,
			Size:      10,
		}},
		Annotations: map[string]string{"software.ocm/main": root.String()},
	}
	index.SchemaVersion = 2
	idxJSON, err := json.Marshal(index)
	require.NoError(t, err)

	archive := func(descriptorName string, gzipIt bool) []byte {
		var raw bytes.Buffer
		tw := stdtar.NewWriter(&raw)
		require.NoError(t, tw.WriteHeader(&stdtar.Header{Name: descriptorName, Size: int64(len(idxJSON)), Mode: 0o644}))
		_, err := tw.Write(idxJSON)
		require.NoError(t, err)
		// a non-descriptor blob entry that must be skipped without being read into memory
		require.NoError(t, tw.WriteHeader(&stdtar.Header{Name: "blobs/sha256." + root.Encoded(), Size: 3, Mode: 0o644}))
		_, err = tw.Write([]byte("xxx"))
		require.NoError(t, err)
		require.NoError(t, tw.Close())
		if !gzipIt {
			return raw.Bytes()
		}
		var gz bytes.Buffer
		gw := gzip.NewWriter(&gz)
		_, err = gw.Write(raw.Bytes())
		require.NoError(t, err)
		require.NoError(t, gw.Close())
		return gz.Bytes()
	}

	cases := []struct {
		name       string
		descriptor string
		gzip       bool
	}{
		{name: "OCI format index.json", descriptor: "index.json"},
		{name: "OCM format artifact-descriptor.json", descriptor: "artifact-descriptor.json"},
		{name: "gzip-wrapped index.json", descriptor: "index.json", gzip: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			roots, err := ocitar.ArtifactSetRoots(inmemory.New(bytes.NewReader(archive(tc.descriptor, tc.gzip))))
			r.NoError(err)
			r.Contains(roots, root, "the declared main artifact must be returned as a root")
		})
	}

	t.Run("archive without a descriptor errors", func(t *testing.T) {
		r := require.New(t)
		var raw bytes.Buffer
		tw := stdtar.NewWriter(&raw)
		r.NoError(tw.WriteHeader(&stdtar.Header{Name: "oci-layout", Size: 3, Mode: 0o644}))
		_, err := tw.Write([]byte("{}\n"))
		r.NoError(err)
		r.NoError(tw.Close())
		_, err = ocitar.ArtifactSetRoots(inmemory.New(bytes.NewReader(raw.Bytes())))
		r.Error(err)
	})
}
