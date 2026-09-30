package configuration

import (
	"io"
	"os"
	"path/filepath"
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

func TestAddStdinConfig(t *testing.T) {
	const (
		baseData = `{"attributes":{"source":"base"},"type":"attributes.config.ocm.software"}`
		aData    = `{"attributes":{"source":"a"},"type":"attributes.config.ocm.software"}`
		bData    = `{"attributes":{"source":"b"},"type":"attributes.config.ocm.software"}`
	)
	base := &genericv1.Config{Configurations: []*runtime.Raw{{
		Type: runtime.NewUnversionedType("attributes.config.ocm.software"),
		Data: []byte(baseData),
	}}}

	tests := []struct {
		name     string
		stdin    string
		wantData []string
		wantRest string
		wantErr  string
	}{
		{
			name:     "configuration is applied after the base",
			stdin:    streamConfigA,
			wantData: []string{baseData, aData},
		},
		{
			name:     "unversioned configuration type is accepted",
			stdin:    streamConfigB,
			wantData: []string{baseData, bData},
		},
		{
			name:     "configurations are merged in stream order",
			stdin:    streamConfigA + "---\n" + streamConfigB,
			wantData: []string{baseData, aData, bData},
		},
		{
			name:     "other documents stay on stdin in stream order",
			stdin:    streamTransferSpec + "---\n" + streamConfigA + "---\n" + "kind: second\n",
			wantData: []string{baseData, aData},
			wantRest: streamTransferSpec + "---\n" + "kind: second\n",
		},
		{
			name:     "leading separator and empty documents are ignored",
			stdin:    "---\n" + streamConfigA + "---\n---\n" + streamTransferSpec,
			wantData: []string{baseData, aData},
			wantRest: streamTransferSpec,
		},
		{
			name:     "stdin without configuration is left unchanged",
			stdin:    "---\n" + streamTransferSpec,
			wantData: []string{baseData},
			wantRest: "---\n" + streamTransferSpec,
		},
		{
			name:     "empty stdin keeps the base",
			stdin:    "",
			wantData: []string{baseData},
		},
		{
			name:     "invalid YAML is left unchanged for the command",
			stdin:    streamConfigA + "---\n" + "this is: [not valid\n",
			wantData: []string{baseData},
			wantRest: streamConfigA + "---\n" + "this is: [not valid\n",
		},
		{
			name:    "malformed configuration fails",
			stdin:   "type: generic.config.ocm.software/v1\nconfigurations: notalist\n",
			wantErr: "could not load configuration from stdin",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			cmd := &cobra.Command{}
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

func TestIsPiped(t *testing.T) {
	r := require.New(t)

	r.True(isPiped(strings.NewReader("")), "a reader set with cmd.SetIn counts as piped")

	file, err := os.Create(filepath.Join(t.TempDir(), "stdin"))
	r.NoError(err)
	t.Cleanup(func() { _ = file.Close() })
	r.True(isPiped(file), "a redirected file counts as piped")

	devNull, err := os.Open(os.DevNull)
	r.NoError(err)
	t.Cleanup(func() { _ = devNull.Close() })
	r.False(isPiped(devNull), "a character device, like a terminal, is not read")
}

func TestAddStdinConfigSkipsCommands(t *testing.T) {
	newTree := func() (root, skipped, child, help, normal *cobra.Command) {
		root = &cobra.Command{Use: "ocm"}
		skipped = &cobra.Command{Use: "generate", Annotations: map[string]string{SkipStdinConfigAnnotation: ""}}
		child = &cobra.Command{Use: "docs"}
		help = &cobra.Command{Use: "help"}
		normal = &cobra.Command{Use: "transfer"}
		skipped.AddCommand(child)
		root.AddCommand(skipped, help, normal)
		return root, skipped, child, help, normal
	}
	_, skipped, child, help, normal := newTree()

	tests := []struct {
		name     string
		cmd      *cobra.Command
		wantRead bool
	}{
		{name: "annotated command", cmd: skipped},
		{name: "child of an annotated command", cmd: child},
		{name: "cobra help command", cmd: help},
		{name: "other command reads stdin", cmd: normal, wantRead: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			tt.cmd.SetIn(strings.NewReader(streamConfigA))
			cfg, err := AddStdinConfig(tt.cmd, &genericv1.Config{})
			r.NoError(err)
			if tt.wantRead {
				r.Len(cfg.Configurations, 1)
				return
			}
			r.Empty(cfg.Configurations)
			rest, err := io.ReadAll(tt.cmd.InOrStdin())
			r.NoError(err)
			r.Equal(streamConfigA, string(rest), "stdin must not be touched")
		})
	}
}
