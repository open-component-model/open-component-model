package algorithms_test

import (
	"bytes"
	"crypto"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/algorithms"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// coverageDescriptor carries versioned signing labels and a source, the fields
// some normalisations do not cover.
func coverageDescriptor() *descruntime.Descriptor {
	d := testDescriptor()
	d.Component.Sources = []descruntime.Source{{
		ElementMeta: descruntime.ElementMeta{
			ObjectMeta: descruntime.ObjectMeta{
				Name:    "src",
				Version: "1.0.0",
				Labels:  []descruntime.Label{{Name: "s", Value: []byte(`"v"`), Signing: true, Version: "v1"}},
			},
		},
		Type:   "git",
		Access: &runtime.Raw{Type: runtime.NewVersionedType("gitHub", "v1"), Data: []byte(`{"type":"gitHub/v1"}`)},
	}}
	return d
}

// TestUncovered pins every reported coverage gap to reality: tampering with the
// reported fields keeps the digest, while the default detects it.
func TestUncovered(t *testing.T) {
	tamper := map[string]func(d *descruntime.Descriptor){
		"label.version": func(d *descruntime.Descriptor) {
			d.Component.Labels[0].Version = "v666"
			d.Component.Sources[0].Labels[0].Version = "v666"
		},
		"sources": func(d *descruntime.Descriptor) {
			d.Component.Sources[0].Version = "6.6.6"
			d.Component.Sources[0].Type = "evil"
			d.Component.Sources[0].Labels[0].Value = []byte(`"evil"`)
		},
	}
	tests := []struct {
		algo string
		want string
	}{
		{algo: jsonV1, want: "sources"},
		{algo: v4alpha1, want: "label.version"},
		{algo: v4alpha2},
		{algo: algorithms.LegacyV3},
		{algo: v4alpha3},
	}
	for _, tt := range tests {
		t.Run(tt.algo, func(t *testing.T) {
			r := require.New(t)
			r.Equal(tt.want, algorithms.Uncovered(tt.algo))
			for field, mutate := range tamper {
				original, err := algorithms.Digest(coverageDescriptor(), tt.algo, crypto.SHA256.String())
				r.NoError(err)
				tampered := coverageDescriptor()
				mutate(tampered)
				err = algorithms.Verify(tampered, *original)
				if field == tt.want {
					r.NoError(err, "%s must not cover %s", tt.algo, field)
				} else {
					r.ErrorIs(err, algorithms.ErrDigestMismatch, "%s must cover %s", tt.algo, field)
				}
			}
		})
	}
}

func TestWarnUncovered(t *testing.T) {
	warnFor := func(algo string) string {
		var buf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
		algorithms.WarnUncovered(t.Context(), logger, algo, "component", "c")
		return buf.String()
	}
	r := require.New(t)

	for _, algo := range []string{v4alpha2, algorithms.LegacyV3, v4alpha3} {
		r.Empty(warnFor(algo), "%s covers every field and must not warn", algo)
	}
	out := warnFor(v4alpha1)
	r.Contains(out, "level=WARN")
	r.Contains(out, "uncovered=label.version")
	r.Contains(out, "normalisationAlgorithm="+v4alpha1)
	r.Contains(out, "component=c", "caller attributes identify what is trusted")
	r.Contains(warnFor(jsonV1), "uncovered=sources")
}

func TestVerifyOnly(t *testing.T) {
	r := require.New(t)
	r.True(algorithms.VerifyOnly(jsonV1))
	for _, algo := range []string{v4alpha1, v4alpha2, algorithms.LegacyV3, v4alpha3} {
		r.False(algorithms.VerifyOnly(algo), algo)
	}
}
