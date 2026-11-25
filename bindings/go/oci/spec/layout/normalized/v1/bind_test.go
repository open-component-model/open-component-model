package v1_test

import (
	"testing"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	layout "ocm.software/open-component-model/bindings/go/oci/spec/layout/normalized/v1"
)

func TestVerifyNormalizedLayerMatchesAccess(t *testing.T) {
	access := &descruntime.Descriptor{Meta: descruntime.Meta{Version: "v2"}}
	access.Component.Name, access.Component.Version = "example.org/comp", "1.0.0"
	access.Component.Provider.Name = "internal"
	normalized, err := layout.Normalize(access)
	require.NoError(t, err)

	other := &descruntime.Descriptor{Meta: descruntime.Meta{Version: "v2"}}
	other.Component.Name, other.Component.Version = "example.org/comp", "2.0.0"
	other.Component.Provider.Name = "internal"
	otherNormalized, err := layout.Normalize(other)
	require.NoError(t, err)
	require.Len(t, otherNormalized, len(normalized), "the digest mismatch case must not differ in size")

	layerOf := func(algorithm digest.Algorithm, content []byte) ociImageSpecV1.Descriptor {
		return ociImageSpecV1.Descriptor{Digest: algorithm.FromBytes(content), Size: int64(len(content))}
	}
	const mismatch = "access descriptor does not match signed normalized descriptor"

	for _, tc := range []struct {
		name    string
		layer   ociImageSpecV1.Descriptor
		wantErr string
	}{
		{name: "matching layer", layer: layerOf(digest.SHA256, normalized)},
		{name: "matching layer with sha512 digest", layer: layerOf(digest.SHA512, normalized)},
		{name: "digest mismatch", layer: layerOf(digest.SHA256, otherNormalized), wantErr: mismatch},
		{name: "sha512 digest mismatch", layer: layerOf(digest.SHA512, otherNormalized), wantErr: mismatch},
		{
			name:    "size mismatch",
			layer:   ociImageSpecV1.Descriptor{Digest: digest.FromBytes(normalized), Size: int64(len(normalized)) + 1},
			wantErr: mismatch,
		},
		{
			name:    "unsupported digest algorithm",
			layer:   ociImageSpecV1.Descriptor{Digest: "md5:d41d8cd98f00b204e9800998ecf8427e", Size: int64(len(normalized))},
			wantErr: "invalid digest",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := layout.VerifyNormalizedLayerMatchesAccess(tc.layer, access)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}
