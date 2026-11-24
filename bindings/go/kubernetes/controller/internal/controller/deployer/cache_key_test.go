package deployer

import (
	"testing"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

const testHex = "4b2f38767f5b6f2d0e3c5c6a1b9b1c2c6e8f8a2b9d5c3e4f1a0b7c6d5e4f3a2b"

func testDescriptor(name string) *descriptor.Descriptor {
	return &descriptor.Descriptor{Component: descriptor.Component{ComponentMeta: descriptor.ComponentMeta{
		ObjectMeta: descriptor.ObjectMeta{Name: name, Version: "1.0.0"},
	}}}
}

func TestBuildResourceCacheKey(t *testing.T) {
	const fallback = "ocm.software/app:1.0.0/name=manifest"

	for _, tc := range []struct {
		name   string
		digest *descriptor.Digest
		want   string
	}{
		{
			name:   "verifiable digest",
			digest: &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: descriptor.GenericBlobDigestV1, Value: testHex},
			want:   "sha256:" + testHex,
		},
		{
			name:   "excluded from signature",
			digest: descriptor.NewExcludeFromSignatureDigest(),
			want:   fallback,
		},
		{
			// The value of an excluded digest is never verified, so it must not select cached content.
			name:   "excluded normalisation with a recorded value",
			digest: &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: descriptor.ExcludeFromSignature, Value: testHex},
			want:   fallback,
		},
		{
			name:   "no-digest hash algorithm with a recorded value",
			digest: &descriptor.Digest{HashAlgorithm: descriptor.NoDigest, NormalisationAlgorithm: descriptor.GenericBlobDigestV1, Value: testHex},
			want:   fallback,
		},
		{
			name:   "lower-case markers",
			digest: &descriptor.Digest{HashAlgorithm: "no-digest", NormalisationAlgorithm: "exclude-from-signature", Value: "no-digest"},
			want:   fallback,
		},
		{
			name:   "empty digest",
			digest: &descriptor.Digest{},
			want:   fallback,
		},
		{
			name: "nil digest",
			want: fallback,
		},
		{
			name:   "incomplete digest",
			digest: &descriptor.Digest{Value: testHex},
			want:   fallback,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			res := &descriptor.Resource{Digest: tc.digest}
			r.Equal(tc.want, buildResourceCacheKey(res, testDescriptor("ocm.software/app"), nil, "name=manifest"))
		})
	}
}

func TestBuildResourceCacheKeyExcludedResourcesDoNotShareKey(t *testing.T) {
	r := require.New(t)

	a := &descriptor.Resource{Digest: descriptor.NewExcludeFromSignatureDigest()}
	b := &descriptor.Resource{Digest: descriptor.NewExcludeFromSignatureDigest()}

	r.NotEqual(
		buildResourceCacheKey(a, testDescriptor("ocm.software/a"), nil, "name=manifest"),
		buildResourceCacheKey(b, testDescriptor("ocm.software/b"), nil, "name=manifest"),
	)
}
