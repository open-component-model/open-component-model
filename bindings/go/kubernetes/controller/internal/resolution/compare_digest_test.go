package resolution

import (
	"crypto"
	"log/slog"
	"strings"
	"testing"

	"github.com/go-logr/logr/funcr"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/log"

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

// A reference digest under an older normalisation must warn, recommending an
// upgrade; the default must not.
func TestCompareDigest_WarnsNonDefault(t *testing.T) {
	desc := &descriptor.Descriptor{Meta: descriptor.Meta{Version: "v2"}, Component: descriptor.Component{
		ComponentMeta: descriptor.ComponentMeta{ObjectMeta: descriptor.ObjectMeta{Name: "ocm.software/child", Version: "1.0.0"}},
		Provider:      descriptor.Provider{Name: "ocm.software"},
	}}
	compare := func(t *testing.T, algo string) string {
		t.Helper()
		var logs strings.Builder
		ctx := log.IntoContext(t.Context(), funcr.New(func(prefix, args string) { logs.WriteString(args + "\n") }, funcr.Options{}))
		dg, err := signing.GenerateDigest(ctx, desc, slog.New(slog.DiscardHandler), algo, crypto.SHA256.String())
		require.NoError(t, err)
		_, err = compareDigest(ctx, desc, &v2.Digest{HashAlgorithm: dg.HashAlgorithm, NormalisationAlgorithm: dg.NormalisationAlgorithm, Value: dg.Value})
		require.NoError(t, err)
		return logs.String()
	}
	r := require.New(t)
	r.NotContains(compare(t, signing.DefaultNormalisationAlgorithm), "older normalisation algorithm")
	r.Contains(compare(t, "jsonNormalisation/v4alpha1"), "older normalisation algorithm")
}
