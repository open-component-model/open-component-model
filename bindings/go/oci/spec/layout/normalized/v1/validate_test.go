package v1_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	layout "ocm.software/open-component-model/bindings/go/oci/spec/layout/normalized/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/signing"
)

func TestValidateDigests(t *testing.T) {
	localBlob := &v2.LocalBlob{
		Type:           runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
		LocalReference: "sha256:abc",
		MediaType:      "application/octet-stream",
	}
	noneAccess := &runtime.Raw{Type: runtime.NewUnversionedType(signing.AccessTypeNone)}
	digestWith := func(hash string) *descruntime.Digest {
		return &descruntime.Digest{HashAlgorithm: hash, NormalisationAlgorithm: "genericBlobDigest/v1", Value: "abc"}
	}

	for _, tc := range []struct {
		name      string
		access    runtime.Typed
		digest    *descruntime.Digest
		reference bool
		wantErr   string
	}{
		{name: "digested resource with access", access: localBlob, digest: digestWith("SHA-256")},
		{name: "resource excluded from signature", access: localBlob, digest: descruntime.NewExcludeFromSignatureDigest()},
		{name: "none access without digest", access: noneAccess},
		{name: "missing digest with access", access: localBlob, wantErr: "missing digest in resource"},
		{name: "none access with digest", access: noneAccess, digest: digestWith("SHA-256"), wantErr: "digest for resource with empty access not allowed"},
		{name: "weak hash algorithm", access: localBlob, digest: digestWith("SHA-1"), wantErr: "unsupported digest hash algorithm"},
		{name: "reference without digest", access: localBlob, digest: digestWith("SHA-256"), reference: true, wantErr: "missing digest in componentReference"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			desc := &descruntime.Descriptor{}
			desc.Component.Name, desc.Component.Version = "example.org/comp", "1.0.0"
			res := descruntime.Resource{Type: "blob", Access: tc.access, Digest: tc.digest}
			res.Name, res.Version = "res", "1.0.0"
			desc.Component.Resources = []descruntime.Resource{res}
			if tc.reference {
				var ref descruntime.Reference
				ref.Name, ref.Version, ref.Component = "dep", "1.0.0", "example.org/dep"
				desc.Component.References = []descruntime.Reference{ref}
			}

			err := layout.ValidateDigests(desc)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}
