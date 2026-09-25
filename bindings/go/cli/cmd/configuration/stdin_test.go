package configuration

import (
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
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

func TestAddStdinConfig(t *testing.T) {
	base := &genericv1.Config{Configurations: []*runtime.Raw{{
		Type: runtime.NewUnversionedType("attributes.config.ocm.software"),
		Data: []byte(`{"attributes":{"source":"base"},"type":"attributes.config.ocm.software"}`),
	}}}
	baseData := `{"attributes":{"source":"base"},"type":"attributes.config.ocm.software"}`
	stdinData := `{"attributes":{"source":"a"},"type":"attributes.config.ocm.software"}`

	tests := []struct {
		name     string
		args     []string
		stdin    string
		wantData []string
		wantRest string
		wantErr  string
	}{
		{
			name:     "configuration in stdin is applied after the base",
			args:     []string{"--spec", "-"},
			stdin:    streamConfigA + "---\n" + streamOther,
			wantData: []string{baseData, stdinData},
			wantRest: streamOther,
		},
		{
			name:     "stdin without configuration keeps the base",
			args:     []string{"--spec", "-"},
			stdin:    streamOther,
			wantData: []string{baseData},
			wantRest: streamOther,
		},
		{
			name:     "stdin flag not set to - leaves stdin alone",
			args:     []string{"--spec", "spec.yaml"},
			stdin:    streamConfigA + "---\n" + streamOther,
			wantData: []string{baseData},
			wantRest: streamConfigA + "---\n" + streamOther,
		},
		{
			name:     "--config - already read stdin",
			args:     []string{"--spec", "-", "--config", "-"},
			stdin:    streamConfigA + "---\n" + streamOther,
			wantData: []string{baseData},
			wantRest: streamConfigA + "---\n" + streamOther,
		},
		{
			name:     "--config with a file still applies stdin",
			args:     []string{"--spec", "-", "--config", "some.yaml"},
			stdin:    streamConfigA,
			wantData: []string{baseData, stdinData},
		},
		{
			name:     "invalid YAML is handed back to the command",
			args:     []string{"--spec", "-"},
			stdin:    "this is: [not valid\n",
			wantData: []string{baseData},
			wantRest: "this is: [not valid\n",
		},
		{
			name:    "malformed configuration in stdin fails",
			args:    []string{"--spec", "-"},
			stdin:   "type: generic.config.ocm.software/v1\nconfigurations: notalist\n",
			wantErr: "could not load configuration from stdin",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			cmd := &cobra.Command{}
			cmd.Flags().StringSlice(OCMConfigCommandArgument, nil, "")
			cmd.Flags().String("spec", "", "")
			r.NoError(cmd.Flags().SetAnnotation("spec", StdinFlagAnnotation, []string{"true"}))
			r.NoError(cmd.Flags().Parse(tt.args))
			cmd.SetIn(strings.NewReader(tt.stdin))

			cfg, err := AddStdinConfig(cmd, base)
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
			rest, err := io.ReadAll(cmd.InOrStdin())
			r.NoError(err)
			r.Equal(strings.TrimSpace(tt.wantRest), strings.TrimSpace(string(rest)))
		})
	}
}
