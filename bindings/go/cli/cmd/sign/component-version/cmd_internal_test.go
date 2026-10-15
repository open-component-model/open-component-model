package componentversion

import (
	"testing"

	"github.com/stretchr/testify/require"
)

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
