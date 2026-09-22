package config_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/cli/cmd/internal/test"
	"ocm.software/open-component-model/bindings/go/cli/internal/context"
)

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

func TestGetConfigFromStdin(t *testing.T) {
	r := require.New(t)
	out := new(bytes.Buffer)
	_, err := test.OCM(t,
		test.WithArgs("get", "config", "--config", "-"),
		test.WithInput(bytes.NewBufferString(stdinConfig)),
		test.WithOutput(out),
		test.WithErrorOutput(test.NewJSONLogReader()),
	)
	r.NoError(err)
	r.Contains(out.String(), "stdin.example.com")
}

func TestGetConfigFromStdinMergedWithFile(t *testing.T) {
	r := require.New(t)
	out := new(bytes.Buffer)
	_, err := test.OCM(t,
		test.WithArgs("get", "config", "--config", "testdata/ocmconfig.yaml", "--config", "-"),
		test.WithInput(bytes.NewBufferString(stdinConfig)),
		test.WithOutput(out),
		test.WithErrorOutput(test.NewJSONLogReader()),
	)
	r.NoError(err)
	r.Contains(out.String(), "file.example.com")
	r.Contains(out.String(), "stdin.example.com")
	r.Less(bytes.Index(out.Bytes(), []byte("file.example.com")), bytes.Index(out.Bytes(), []byte("stdin.example.com")),
		"file entry must come before the stdin entry to keep command line order")
}

func TestGetConfigFromStdinInvalid(t *testing.T) {
	_, err := test.OCM(t,
		test.WithArgs("get", "config", "--config", "-"),
		test.WithInput(bytes.NewBufferString("not: [valid: yaml: {")),
		test.WithOutput(new(bytes.Buffer)),
		test.WithErrorOutput(test.NewJSONLogReader()),
	)
	require.ErrorContains(t, err, "stdin")
}

// TestGetConfigFromStdinSkipsDiscovery proves that "--config -" replaces the well known
// locations instead of adding to them. The control run without --config shows that the
// discovered file is otherwise picked up.
func TestGetConfigFromStdinSkipsDiscovery(t *testing.T) {
	r := require.New(t)
	discovered, err := filepath.Abs("testdata/ocmconfig.yaml")
	r.NoError(err)
	syscalls := &context.Syscalls{
		Stat:   os.Stat,
		Getenv: func(key string) string { return map[string]string{"OCM_CONFIG": discovered}[key] },
	}

	control := new(bytes.Buffer)
	_, err = test.OCM(t,
		test.WithArgs("get", "config"),
		test.WithSyscalls(syscalls),
		test.WithOutput(control),
		test.WithErrorOutput(test.NewJSONLogReader()),
	)
	r.NoError(err)
	r.Contains(control.String(), "file.example.com", "control: discovery must find the file via OCM_CONFIG")

	out := new(bytes.Buffer)
	_, err = test.OCM(t,
		test.WithArgs("get", "config", "--config", "-"),
		test.WithSyscalls(syscalls),
		test.WithInput(bytes.NewBufferString(stdinConfig)),
		test.WithOutput(out),
		test.WithErrorOutput(test.NewJSONLogReader()),
	)
	r.NoError(err)
	r.Contains(out.String(), "stdin.example.com")
	r.NotContains(out.String(), "file.example.com", "discovered config must be ignored when --config - is given")
}
