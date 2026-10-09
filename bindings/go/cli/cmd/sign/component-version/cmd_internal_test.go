package componentversion

import (
	"testing"

	"github.com/stretchr/testify/require"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/signing/tsa"
)

func TestSetTSALabel(t *testing.T) {
	tsaLabel := func(url string) descruntime.Label {
		return descruntime.Label{Name: tsa.TSAURLLabelPrefix + "default", Value: []byte(`"` + url + `"`), Signing: true, Version: "v1alpha1"}
	}
	otherLabel := descruntime.Label{Name: "other", Value: []byte(`"x"`)}

	tests := []struct {
		name       string
		tsaURL     string
		labels     []descruntime.Label
		signatures []descruntime.Signature
		wantLabels []descruntime.Label
		wantErr    string
	}{
		{
			name:       "adds a signed label with only scheme, host, port and path",
			tsaURL:     "https://user:token@tsa.example:8443/ts?apikey=secret#frag",
			wantLabels: []descruntime.Label{tsaLabel("https://tsa.example:8443/ts")},
		},
		{
			name:       "replaces the label of a re-signed signature in place",
			tsaURL:     "https://new.example/ts",
			labels:     []descruntime.Label{tsaLabel("https://old.example/ts"), otherLabel},
			signatures: []descruntime.Signature{{Name: "default"}},
			wantLabels: []descruntime.Label{tsaLabel("https://new.example/ts"), otherLabel},
		},
		{
			name:       "removes a stale label when re-signed without a TSA",
			labels:     []descruntime.Label{tsaLabel("https://old.example/ts"), otherLabel},
			signatures: []descruntime.Signature{{Name: "default"}},
			wantLabels: []descruntime.Label{otherLabel},
		},
		{
			name:       "nothing to remove",
			labels:     []descruntime.Label{otherLabel},
			signatures: []descruntime.Signature{{Name: "other"}},
			wantLabels: []descruntime.Label{otherLabel},
		},
		{
			name:       "refuses to add when another signature covers the descriptor",
			tsaURL:     "https://tsa.example/ts",
			labels:     []descruntime.Label{otherLabel},
			signatures: []descruntime.Signature{{Name: "other"}},
			wantLabels: []descruntime.Label{otherLabel},
			wantErr:    `would invalidate the existing signature "other"`,
		},
		{
			name:       "refuses to remove when another signature covers the label",
			labels:     []descruntime.Label{tsaLabel("https://old.example/ts")},
			signatures: []descruntime.Signature{{Name: "default"}, {Name: "other"}},
			wantLabels: []descruntime.Label{tsaLabel("https://old.example/ts")},
			wantErr:    `would invalidate the existing signature "other"`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			desc := &descruntime.Descriptor{Signatures: tc.signatures}
			desc.Component.Labels = tc.labels
			err := setTSALabel(desc, "default", tc.tsaURL)
			if tc.wantErr != "" {
				r.ErrorContains(err, tc.wantErr)
			} else {
				r.NoError(err)
			}
			r.Equal(tc.wantLabels, desc.Component.Labels)
		})
	}
}

func TestEffectiveTSAURL(t *testing.T) {
	tests := []struct {
		name       string
		useDefault bool
		customURL  string
		want       string
	}{
		{name: "no timestamp"},
		{name: "default TSA", useDefault: true, want: DefaultTSAURL},
		{name: "custom TSA implies --tsa", customURL: "https://tsa.example/ts", want: "https://tsa.example/ts"},
		{name: "custom TSA overrides the default", useDefault: true, customURL: "https://tsa.example/ts", want: "https://tsa.example/ts"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.New(t).Equal(tc.want, effectiveTSAURL(tc.useDefault, tc.customURL))
		})
	}
}
