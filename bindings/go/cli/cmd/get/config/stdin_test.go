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

func TestGetConfigFromStdinMergedWithFile(t *testing.T) {
	r := require.New(t)
	out := new(bytes.Buffer)
	_, err := test.OCM(t,
		test.WithArgs("get", "config", "--config", "testdata/ocmconfig.yaml"),
		test.WithInput(bytes.NewBufferString(stdinConfig)),
		test.WithOutput(out),
		test.WithErrorOutput(test.NewJSONLogReader()),
	)
	r.NoError(err)
	r.Contains(out.String(), "file.example.com")
	r.Contains(out.String(), "stdin.example.com")
	r.Less(bytes.Index(out.Bytes(), []byte("file.example.com")), bytes.Index(out.Bytes(), []byte("stdin.example.com")),
		"stdin configuration must come last, so it has the highest priority")
}

// TestGetConfigFromStdinMergesWithDiscovery proves that piped configuration adds to the
// configuration found in the well known locations instead of replacing it.
func TestGetConfigFromStdinMergesWithDiscovery(t *testing.T) {
	r := require.New(t)
	discovered, err := filepath.Abs("testdata/ocmconfig.yaml")
	r.NoError(err)
	syscalls := &context.Syscalls{
		Stat:   os.Stat,
		Getenv: func(key string) string { return map[string]string{"OCM_CONFIG": discovered}[key] },
	}

	out := new(bytes.Buffer)
	_, err = test.OCM(t,
		test.WithArgs("get", "config"),
		test.WithSyscalls(syscalls),
		test.WithInput(bytes.NewBufferString(stdinConfig)),
		test.WithOutput(out),
		test.WithErrorOutput(test.NewJSONLogReader()),
	)
	r.NoError(err)
	r.Contains(out.String(), "file.example.com")
	r.Contains(out.String(), "stdin.example.com")
}
