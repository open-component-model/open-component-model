package configuration

import (
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"
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
	// streamTransferSpec carries nested "type" fields, which must not make it look like a
	// configuration document.
	streamTransferSpec = `environment: {}
transformations:
- id: upload
  spec:
    repository:
      type: OCIRepository/v1
  type: OCIAddComponentVersion/v1alpha1
`
)

func TestReadStdinConfigs(t *testing.T) {
	const (
		aData = `{"attributes":{"source":"a"},"type":"attributes.config.ocm.software"}`
		bData = `{"attributes":{"source":"b"},"type":"attributes.config.ocm.software"}`
	)

	tests := []struct {
		name     string
		stdin    string
		wantData []string
		wantRest string
		wantErr  string
	}{
		{
			name:     "single configuration",
			stdin:    streamConfigA,
			wantData: []string{aData},
		},
		{
			name:     "unversioned configuration type is accepted",
			stdin:    streamConfigB,
			wantData: []string{bData},
		},
		{
			name:     "configurations keep stream order",
			stdin:    streamConfigA + "---\n" + streamConfigB,
			wantData: []string{aData, bData},
		},
		{
			name:     "other documents stay on stdin in stream order",
			stdin:    streamTransferSpec + "---\n" + streamConfigA + "---\n" + "kind: second\n",
			wantData: []string{aData},
			wantRest: streamTransferSpec + "---\n" + "kind: second\n",
		},
		{
			name:     "leading separator and empty documents are ignored",
			stdin:    "---\n" + streamConfigA + "---\n---\n" + streamTransferSpec,
			wantData: []string{aData},
			wantRest: streamTransferSpec,
		},
		{
			name:    "stdin without configuration fails",
			stdin:   "---\n" + streamTransferSpec,
			wantErr: "no configuration document found",
		},
		{
			name:    "empty stdin fails",
			stdin:   "",
			wantErr: "no configuration document found",
		},
		{
			name:    "invalid YAML fails",
			stdin:   streamConfigA + "---\n" + "this is: [not valid\n",
			wantErr: "stdin:",
		},
		{
			name:    "malformed configuration fails",
			stdin:   "type: generic.config.ocm.software/v1\nconfigurations: notalist\n",
			wantErr: "stdin:",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			cmd := &cobra.Command{}
			cmd.SetIn(strings.NewReader(tt.stdin))

			cfgs, err := readStdinConfigs(cmd)
			if tt.wantErr != "" {
				r.ErrorContains(err, tt.wantErr)
				return
			}
			r.NoError(err)
			var got []string
			for _, cfg := range cfgs {
				for _, c := range cfg.Configurations {
					got = append(got, string(c.Data))
				}
			}
			r.Equal(tt.wantData, got)
			rest, err := io.ReadAll(cmd.InOrStdin())
			r.NoError(err)
			r.Equal(strings.TrimSpace(tt.wantRest), strings.TrimSpace(string(rest)))
		})
	}
}
