package resolution

import (
	"crypto"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/signing"
)

// compareDigest must accept every spelling the other verifiers accept: hex
// values are case-insensitive, as in transfer and the constructor.
func TestCompareDigest(t *testing.T) {
	desc := &descriptor.Descriptor{Meta: descriptor.Meta{Version: "v2"}, Component: descriptor.Component{
		ComponentMeta: descriptor.ComponentMeta{ObjectMeta: descriptor.ObjectMeta{Name: "ocm.software/child", Version: "1.0.0"}},
		Provider:      descriptor.Provider{Name: "ocm.software"},
	}}
	dg, err := signing.GenerateDigest(t.Context(), desc, slog.New(slog.DiscardHandler), signing.DefaultNormalisationAlgorithm, crypto.SHA256.String())
	require.NoError(t, err)

	tests := []struct {
		name    string
		digest  v2.Digest
		wantErr bool
	}{
		{name: "canonical", digest: v2.Digest{HashAlgorithm: dg.HashAlgorithm, NormalisationAlgorithm: dg.NormalisationAlgorithm, Value: dg.Value}},
		{name: "uppercase hex", digest: v2.Digest{HashAlgorithm: dg.HashAlgorithm, NormalisationAlgorithm: dg.NormalisationAlgorithm, Value: strings.ToUpper(dg.Value)}},
		{name: "legacy hash name", digest: v2.Digest{HashAlgorithm: "sha256", NormalisationAlgorithm: dg.NormalisationAlgorithm, Value: dg.Value}},
		{name: "mismatch", digest: v2.Digest{HashAlgorithm: dg.HashAlgorithm, NormalisationAlgorithm: dg.NormalisationAlgorithm, Value: strings.Repeat("0", 64)}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			_, err := compareDigest(t.Context(), desc, &tt.digest)
			if tt.wantErr {
				r.ErrorContains(err, "digest mismatch")
				return
			}
			r.NoError(err)
		})
	}
}
