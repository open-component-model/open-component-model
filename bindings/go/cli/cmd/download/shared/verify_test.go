package shared_test

import (
	"bytes"
	"crypto/sha512"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/memory"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/cli/cmd/download/shared"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/tar"
)

// A SHA-512 genericBlobDigest/v1 is a valid, signable resource digest, so a pinned
// download of matching content verifies, and of other content fails.
func TestVerifyResourceContent_SHA512BlobDigest(t *testing.T) {
	r := require.New(t)
	data := []byte("pinned content")
	sum := sha512.Sum512(data)
	res := &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "blob", Version: "1.0.0"}},
		Digest: &descriptor.Digest{
			HashAlgorithm:          "SHA-512",
			NormalisationAlgorithm: "genericBlobDigest/v1",
			Value:                  hex.EncodeToString(sum[:]),
		},
	}
	read := func(content []byte) ([]byte, error) {
		verified, err := shared.VerifyResourceContent(t.Context(), res, inmemory.New(bytes.NewReader(content)))
		if err != nil {
			return nil, err
		}
		rc, err := verified.ReadCloser()
		if err != nil {
			return nil, err
		}
		got, err := io.ReadAll(rc)
		if closeErr := rc.Close(); err == nil {
			err = closeErr
		}
		return got, err
	}

	got, err := read(data)
	r.NoError(err)
	r.Equal(data, got)
	_, err = read([]byte("substituted content"))
	r.Error(err, "content not matching its SHA-512 digest must fail")
}

// repository.VerifyDownload accepts a digest value spelled "<algorithm>:<hex>"; OCI artifacts
// verified against ociArtifactDigest/v1 accept the same spelling.
func TestVerifyResourceContent_PrefixedOCIArtifactDigest(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	store := memory.New()
	layer := []byte("layer")
	layerDesc := ociImageSpecV1.Descriptor{MediaType: "application/octet-stream", Digest: digest.FromBytes(layer), Size: int64(len(layer))}
	r.NoError(store.Push(ctx, layerDesc, bytes.NewReader(layer)))
	manifest, err := oras.PackManifest(ctx, store, oras.PackManifestVersion1_1, "application/artifact", oras.PackManifestOptions{
		Layers: []ociImageSpecV1.Descriptor{layerDesc},
	})
	r.NoError(err)
	layout, err := tar.CopyToOCILayoutInMemory(ctx, store, manifest, tar.CopyToOCILayoutOptions{Tags: []string{"v1"}})
	r.NoError(err)

	res := &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "image", Version: "1.0.0"}},
		Digest: &descriptor.Digest{
			HashAlgorithm:          "SHA-256",
			NormalisationAlgorithm: "ociArtifactDigest/v1",
			Value:                  manifest.Digest.String(),
		},
	}
	_, err = shared.VerifyResourceContent(ctx, res, layout)
	r.NoError(err)

	res.Digest.Value = "sha512:" + manifest.Digest.Encoded()
	_, err = shared.VerifyResourceContent(ctx, res, layout)
	r.ErrorContains(err, "carries algorithm")
}

// A digest-pinned download of an OCI artifact that OCM v1 stored as an artifact set verifies its
// main manifest and every blob it references, although the set stores its blobs flat.
func TestVerifyResourceContent_V1ArtifactSet(t *testing.T) {
	r := require.New(t)
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "ctf", "testdata", "compatibility", "02", "with-resource", "blobs",
		"sha256.e40e3a2f1ab1a98328dfd14539a79d27aff5c4d5c34cd16a85f0288bfa76490b"))
	r.NoError(err)

	for _, tc := range []struct {
		name    string
		value   string
		wantErr string
	}{
		{name: "matching manifest digest", value: "62be4af3382a4493cb7f1dd4ec47bcb28f1863b615fc9e4a1dceefbe93898dd0"},
		{name: "other manifest digest", value: strings.Repeat("ab", 32), wantErr: "OCI artifact digest mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			res := &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "chart", Version: "1.0.0"}},
				Digest:      &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "ociArtifactDigest/v1", Value: tc.value},
			}
			_, err := shared.VerifyResourceContent(t.Context(), res, inmemory.New(bytes.NewReader(data)))
			if tc.wantErr != "" {
				r.ErrorContains(err, tc.wantErr)
				return
			}
			r.NoError(err)
		})
	}
}
