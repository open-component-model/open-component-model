package ocm

import (
	"testing"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestIsLocalBlob(t *testing.T) {
	r := require.New(t)
	r.True(IsLocalBlob(&descriptor.LocalBlob{}))
	r.True(IsLocalBlob(&runtime.Raw{Type: runtime.NewUnversionedType(descriptor.LocalBlobAccessType)}))
	r.True(IsLocalBlob(&runtime.Raw{Type: runtime.NewUnversionedType(descriptor.LegacyLocalBlobAccessType)}))
	r.False(IsLocalBlob(&runtime.Raw{Type: runtime.NewUnversionedType("OCIImage")}))
	r.False(IsLocalBlob(nil))
}

func TestIsExcludedFromSignature(t *testing.T) {
	r := require.New(t)
	r.True(IsExcludedFromSignature(descriptor.NewExcludeFromSignatureDigest()))
	r.False(IsExcludedFromSignature(&descriptor.Digest{
		HashAlgorithm:          "SHA-256",
		NormalisationAlgorithm: "genericBlobDigest/v1",
		Value:                  "abc",
	}))
	r.False(IsExcludedFromSignature(nil))
}
