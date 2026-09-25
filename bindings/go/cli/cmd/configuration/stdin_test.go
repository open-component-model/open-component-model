package configuration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	streamConfigA = `type: generic.config.ocm.software/v1
configurations:
- type: attributes.config.ocm.software
  attributes:
    source: a
`
	streamConfigB = `type: generic.config.ocm.software
configurations:
- type: attributes.config.ocm.software
  attributes:
    source: b
`
	streamOther = `environment: {}
transformations: []
`
)

func TestReadConfigStream(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		wantData []string
		wantRest string
		wantErr  string
	}{
		{
			name:     "single configuration",
			in:       streamConfigA,
			wantData: []string{`{"attributes":{"source":"a"},"type":"attributes.config.ocm.software"}`},
		},
		{
			name:     "unversioned configuration type is accepted",
			in:       streamConfigB,
			wantData: []string{`{"attributes":{"source":"b"},"type":"attributes.config.ocm.software"}`},
		},
		{
			name: "configurations are merged in stream order",
			in:   streamConfigA + "---\n" + streamConfigB,
			wantData: []string{
				`{"attributes":{"source":"a"},"type":"attributes.config.ocm.software"}`,
				`{"attributes":{"source":"b"},"type":"attributes.config.ocm.software"}`,
			},
		},
		{
			name:     "other documents are returned in stream order",
			in:       streamOther + "---\n" + streamConfigA + "---\n" + "kind: second\n",
			wantData: []string{`{"attributes":{"source":"a"},"type":"attributes.config.ocm.software"}`},
			wantRest: streamOther + "---\n" + "kind: second\n",
		},
		{
			name:     "leading separator and empty documents are ignored",
			in:       "---\n" + streamConfigA + "---\n---\n" + streamOther,
			wantData: []string{`{"attributes":{"source":"a"},"type":"attributes.config.ocm.software"}`},
			wantRest: streamOther,
		},
		{
			name:    "other type is not a configuration",
			in:      "type: something.else/v1\n",
			wantErr: "no configuration document",
		},
		{
			name:    "empty input",
			in:      "",
			wantErr: "no data was read",
		},
		{
			name:    "malformed document fails the whole stream",
			in:      streamConfigA + "---\n" + "this is: [not valid\n",
			wantErr: "error converting YAML to JSON",
		},
		{
			name:    "malformed configuration document reports the decoding error",
			in:      "type: generic.config.ocm.software/v1\nconfigurations: notalist\n",
			wantErr: "configurations",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			cfg, rest, err := readConfigStream(strings.NewReader(tt.in))
			if tt.wantErr != "" {
				r.ErrorContains(err, tt.wantErr)
				return
			}
			r.NoError(err)
			got := make([]string, 0, len(cfg.Configurations))
			for _, c := range cfg.Configurations {
				got = append(got, string(c.Data))
			}
			r.Equal(tt.wantData, got)
			r.Equal(strings.TrimSpace(tt.wantRest), strings.TrimSpace(string(rest)))
		})
	}
}
