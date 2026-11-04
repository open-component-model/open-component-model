package config_test

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/cli/cmd/get/config"
	"ocm.software/open-component-model/bindings/go/cli/cmd/internal/test"
	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	versioningv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/versioning/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const versioningEntry = `{
  "type": "versioning.config.ocm.software/v1alpha1",
  "schemes": [
    {
      "name": "calver-full",
      "pattern": "^(?P<year>\\d{4})\\.(?P<month>\\d{2})\\.(?P<day>\\d{2})$",
      "comparisonGroups": ["year", "month", "day"]
    }
  ]
}`

// TestGetEffectiveConfig_IncludesVersioning verifies that a configured versioning
// scheme is surfaced in the effective configuration, so "ocm get config" proves
// the active scheme rather than silently omitting it.
func TestGetEffectiveConfig_IncludesVersioning(t *testing.T) {
	r := require.New(t)

	raw := &runtime.Raw{}
	r.NoError(raw.UnmarshalJSON([]byte(versioningEntry)))
	cfg := &genericv1.Config{
		Type:           runtime.NewVersionedType(genericv1.ConfigType, genericv1.ConfigTypeV1),
		Configurations: []*runtime.Raw{raw},
	}

	eff, err := config.GetEffectiveConfig(cfg)
	r.NoError(err)

	var found *versioningv1alpha1.Config
	for _, entry := range eff.Configurations {
		if vc, ok := entry.(*versioningv1alpha1.Config); ok {
			found = vc
			break
		}
	}
	r.NotNil(found, "versioning config must appear in effective configuration")
	r.Len(found.Schemes, 1)
	r.Equal("calver-full", found.Schemes[0].Name)
}

// TestGetEffectiveConfig_OmitsVersioningWhenAbsent ensures the versioning entry
// is not synthesized when the user configured no versioning schemes.
func TestGetEffectiveConfig_OmitsVersioningWhenAbsent(t *testing.T) {
	r := require.New(t)

	cfg := &genericv1.Config{
		Type:           runtime.NewVersionedType(genericv1.ConfigType, genericv1.ConfigTypeV1),
		Configurations: []*runtime.Raw{},
	}

	eff, err := config.GetEffectiveConfig(cfg)
	r.NoError(err)
	for _, entry := range eff.Configurations {
		_, ok := entry.(*versioningv1alpha1.Config)
		r.False(ok, "versioning config must be absent without configured schemes")
	}
}

const stdinConfig = `type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: OCIRegistry
      hostname: stdin.example.com
    credentials:
    - type: Credentials/v1
      properties:
        username: from-stdin
        password: stdin-secret
`

func TestGetConfig_ConfigFlagWithDashReadsStdin(t *testing.T) {
	r := require.New(t)
	out := new(bytes.Buffer)
	_, err := test.OCM(t,
		test.WithArgs("get", "config", "--config", "-"),
		test.WithInput(strings.NewReader(stdinConfig)),
		test.WithOutput(out),
		test.WithErrorOutput(test.NewJSONLogReader()),
	)
	r.NoError(err)
	r.Contains(out.String(), "stdin.example.com")
}

// TestGetConfig_ConfigFlagsMergeInGivenOrder proves that "-" is an ordinary --config
// entry: stdin and files merge in the order given, and a later entry wins.
func TestGetConfig_ConfigFlagsMergeInGivenOrder(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		first string
		last  string
	}{
		{
			name:  "file then stdin",
			args:  []string{"--config", "testdata/ocmconfig.yaml", "--config", "-"},
			first: "file.example.com",
			last:  "stdin.example.com",
		},
		{
			name:  "stdin then file",
			args:  []string{"--config", "-", "--config", "testdata/ocmconfig.yaml"},
			first: "stdin.example.com",
			last:  "file.example.com",
		},
		{
			name:  "repeated stdin entry is ignored",
			args:  []string{"--config", "-", "--config", "testdata/ocmconfig.yaml", "--config", "-"},
			first: "stdin.example.com",
			last:  "file.example.com",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			out := new(bytes.Buffer)
			_, err := test.OCM(t,
				test.WithArgs(append([]string{"get", "config"}, tt.args...)...),
				test.WithInput(strings.NewReader(stdinConfig)),
				test.WithOutput(out),
				test.WithErrorOutput(test.NewJSONLogReader()),
			)
			r.NoError(err)
			first := strings.Index(out.String(), tt.first)
			last := strings.Index(out.String(), tt.last)
			r.NotEqual(-1, first)
			r.NotEqual(-1, last)
			r.Less(first, last, "--config entries are merged in the order given, later ones win")
		})
	}
}

// TestGetConfig_OpenStdinIsNotReadWithoutDash proves that stdin is left alone unless
// asked for with --config -. The pipe mirrors a CI wrapper such as "devcontainer exec":
// the write end stays open and never sends EOF, so a command that reads stdin blocks
// forever. A buffer could not show this, because reading a buffer returns at once.
func TestGetConfig_OpenStdinIsNotReadWithoutDash(t *testing.T) {
	r := require.New(t)
	reader, writer, err := os.Pipe()
	r.NoError(err)
	t.Cleanup(func() { _ = reader.Close() })
	t.Cleanup(func() { _ = writer.Close() })
	// Data in the pipe but no EOF: reading would block, and anything read is lost
	// for the check below.
	_, err = writer.WriteString(stdinConfig)
	r.NoError(err)

	out := new(bytes.Buffer)
	done := make(chan error, 1)
	go func() {
		_, err := test.OCM(t,
			test.WithArgs("get", "config"),
			test.WithInput(reader),
			test.WithOutput(out),
			test.WithErrorOutput(test.NewJSONLogReader()),
		)
		done <- err
	}()
	select {
	case err := <-done:
		r.NoError(err)
	case <-time.After(10 * time.Second):
		r.FailNow("command blocked on an open stdin it was not asked to read")
	}

	r.NotContains(out.String(), "stdin.example.com")
	r.NoError(writer.Close())
	rest, err := io.ReadAll(reader)
	r.NoError(err)
	r.Equal(stdinConfig, string(rest), "stdin must not be read")
}
