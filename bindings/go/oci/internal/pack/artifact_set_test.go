package pack_test

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	. "ocm.software/open-component-model/bindings/go/oci/internal/pack"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/memory"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	resourceblob "ocm.software/open-component-model/bindings/go/oci/blob"
)

const artifactSetMediaType = "application/vnd.oci.image.manifest.v1+tar+gzip"

// OCM v1 stores an OCI artifact in a local blob as an artifact set, whose ociArtifactDigest/v1
// digest describes its main manifest. Packing keeps the archive as it is and checks the digest
// against the manifest and every blob the manifest references.
func TestPackingVerifiesArtifactSetDigest(t *testing.T) {
	archive, manifest := newArtifactSet(t, false)
	corrupt, _ := newArtifactSet(t, true)
	ociDigest := func(value string) *descriptor.Digest {
		return &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "ociArtifactDigest/v1", Value: value}
	}

	for _, tc := range []struct {
		name    string
		data    []byte
		digest  *descriptor.Digest
		want    *descriptor.Digest
		wantErr string
	}{
		{name: "matching manifest digest is kept", data: archive, digest: ociDigest(manifest.Encoded()), want: ociDigest(manifest.Encoded())},
		{name: "missing digest is generated from the manifest", data: archive, want: ociDigest(manifest.Encoded())},
		{name: "other manifest digest is rejected", data: archive, digest: ociDigest(strings.Repeat("ab", 32)), wantErr: "digest value mismatch"},
		{name: "corrupt layer is rejected", data: corrupt, digest: ociDigest(manifest.Encoded()), wantErr: "failed to verify artifact set"},
		{
			name:   "excluded resource is not read",
			data:   []byte("not an artifact set"),
			digest: descriptor.NewExcludeFromSignatureDigest(),
			want:   descriptor.NewExcludeFromSignatureDigest(),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			access := &v2.LocalBlob{MediaType: artifactSetMediaType}
			resource := &descriptor.Resource{Access: access, Digest: tc.digest}
			b := &resourceblob.ArtifactBlob{
				Artifact:     resource,
				ReadOnlyBlob: &testBlob{content: tc.data, mediaType: artifactSetMediaType, digest: digest.FromBytes(tc.data)},
			}

			packed, err := ArtifactBlob(t.Context(), memory.New(), b, Options{AccessScheme: v2.Scheme})
			if tc.wantErr != "" {
				r.ErrorContains(err, tc.wantErr)
				r.Equal(tc.digest, resource.Digest)
				return
			}
			r.NoError(err)
			r.Equal(digest.FromBytes(tc.data), packed.Digest, "the archive is packed as it is")
			r.Equal(tc.want, resource.Digest)
			updated, ok := resource.Access.(*v2.LocalBlob)
			r.True(ok)
			r.Equal(digest.FromBytes(tc.data).String(), updated.LocalReference)
			r.Equal(artifactSetMediaType, updated.MediaType)
		})
	}
}

// newArtifactSet writes an OCM v1 artifact set holding one image: an OCI layout whose blobs are
// stored flat as blobs/<algorithm>.<encoded>. corrupt stores other bytes under the layer digest.
func newArtifactSet(t *testing.T, corrupt bool) ([]byte, digest.Digest) {
	t.Helper()
	r := require.New(t)
	layerData := []byte("layer content")
	layer := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageLayer, layerData)
	manifestData, err := json.Marshal(ociImageSpecV1.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Config:    ociImageSpecV1.DescriptorEmptyJSON,
		Layers:    []ociImageSpecV1.Descriptor{layer},
	})
	r.NoError(err)
	manifest := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageManifest, manifestData)
	indexData, err := json.Marshal(ociImageSpecV1.Index{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ociImageSpecV1.MediaTypeImageIndex,
		Manifests: []ociImageSpecV1.Descriptor{manifest},
	})
	r.NoError(err)
	if corrupt {
		layerData = []byte("other content")
	}

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	write := func(name string, data []byte) {
		r.NoError(tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg}))
		_, err := tw.Write(data)
		r.NoError(err)
	}
	write(ociImageSpecV1.ImageLayoutFile, []byte(`{"imageLayoutVersion":"1.0.0"}`))
	write(ociImageSpecV1.ImageIndexFile, indexData)
	r.NoError(tw.WriteHeader(&tar.Header{Name: ociImageSpecV1.ImageBlobsDir + "/", Mode: 0o755, Typeflag: tar.TypeDir}))
	for _, blob := range []struct {
		desc ociImageSpecV1.Descriptor
		data []byte
	}{
		{desc: manifest, data: manifestData},
		{desc: ociImageSpecV1.DescriptorEmptyJSON, data: ociImageSpecV1.DescriptorEmptyJSON.Data},
		{desc: layer, data: layerData},
	} {
		write(ociImageSpecV1.ImageBlobsDir+"/"+blob.desc.Digest.Algorithm().String()+"."+blob.desc.Digest.Encoded(), blob.data)
	}
	r.NoError(tw.Close())
	return buf.Bytes(), manifest.Digest
}
