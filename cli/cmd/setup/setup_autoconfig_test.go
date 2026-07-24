package setup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/cli/cmd/configuration"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
)

// newTestCommand builds a command whose syscalls are backed by the given HOME directory and env.
func newTestCommand(t *testing.T, home string, env map[string]string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "test"}
	cmd.SetContext(t.Context())
	configuration.RegisterConfigFlag(cmd)
	cmd.SetContext(ocmctx.WithSyscalls(cmd.Context(), &ocmctx.Syscalls{
		Stat: os.Stat,
		Getenv: func(k string) string {
			return env[k]
		},
		UserHomeDir: func() (string, error) { return home, nil },
		Getwd:       func() (string, error) { return home, nil },
		Executable:  func() (string, error) { return filepath.Join(home, "ocm"), nil },
	}))
	return cmd
}

func writeDockerConfig(t *testing.T, dir string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o700))
	path := filepath.Join(dir, "config.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"auths":{"ghcr.io":{"auth":"dXNlcjpwYXNz"}}}`), 0o600))
	return path
}

func TestAutoConfigure_WritesConfigFromDockerConfig(t *testing.T) {
	r := require.New(t)
	home := t.TempDir()
	dockerPath := writeDockerConfig(t, filepath.Join(home, ".docker"))

	cmd := newTestCommand(t, home, map[string]string{})
	r.NoError(AutoConfigure(cmd))

	target := filepath.Join(home, configuration.OCMConfigFileName)
	data, err := os.ReadFile(target)
	r.NoError(err)

	// The written config must be parseable.
	cfg, err := configuration.GetConfigFromPath(target)
	r.NoError(err)
	r.Len(cfg.Configurations, 1)

	// The written config must be discoverable by the regular lookup.
	opts := configuration.OCMConfigOptions{
		Stat: os.Stat, Getenv: func(string) string { return "" },
		UserHomeDir: func() (string, error) { return home, nil },
		Getwd:       func() (string, error) { return home, nil },
		Executable:  func() (string, error) { return filepath.Join(home, "ocm"), nil },
	}
	paths, err := configuration.GetOCMConfigPaths(opts)
	r.NoError(err)
	r.Contains(paths, target)

	// It must reference the detected docker config path.
	r.Contains(string(data), dockerPath)
	r.Contains(string(data), "DockerConfig")
	r.Contains(string(data), "credentials.config.ocm.software")
}

func TestAutoConfigure_RespectsXDGConfigHome(t *testing.T) {
	r := require.New(t)
	home := t.TempDir()
	xdg := t.TempDir()
	writeDockerConfig(t, filepath.Join(home, ".docker"))

	cmd := newTestCommand(t, home, map[string]string{xdgConfigHomeEnvVar: xdg})
	r.NoError(AutoConfigure(cmd))

	target := filepath.Join(xdg, configuration.OCMConfigFileName)
	_, err := os.Stat(target)
	r.NoError(err)
}

func TestAutoConfigure_DetectsDockerConfigEnvVar(t *testing.T) {
	r := require.New(t)
	home := t.TempDir()
	dockerDir := t.TempDir()
	dockerPath := writeDockerConfig(t, dockerDir)

	cmd := newTestCommand(t, home, map[string]string{dockerConfigEnvVar: dockerDir})
	r.NoError(AutoConfigure(cmd))

	target := filepath.Join(home, configuration.OCMConfigFileName)
	data, err := os.ReadFile(target)
	r.NoError(err)
	r.Contains(string(data), dockerPath)
}

func TestAutoConfigure_NoDockerConfig(t *testing.T) {
	r := require.New(t)
	home := t.TempDir()

	cmd := newTestCommand(t, home, map[string]string{})
	r.NoError(AutoConfigure(cmd))

	_, err := os.Stat(filepath.Join(home, configuration.OCMConfigFileName))
	r.True(os.IsNotExist(err))
}

func TestAutoConfigure_SkipsWhenConfigExists(t *testing.T) {
	r := require.New(t)
	home := t.TempDir()
	writeDockerConfig(t, filepath.Join(home, ".docker"))

	// Pre-existing config in a discoverable location.
	existing := filepath.Join(home, configuration.NestedOCMConfigFileName)
	r.NoError(os.WriteFile(existing, []byte("type: generic.config.ocm.software/v1\nconfigurations: []\n"), 0o600))

	cmd := newTestCommand(t, home, map[string]string{})
	r.NoError(AutoConfigure(cmd))

	// The auto-config target must not have been created.
	_, err := os.Stat(filepath.Join(home, configuration.OCMConfigFileName))
	r.True(os.IsNotExist(err))
}

func TestAutoConfigure_DisabledViaEnv(t *testing.T) {
	r := require.New(t)
	home := t.TempDir()
	writeDockerConfig(t, filepath.Join(home, ".docker"))

	cmd := newTestCommand(t, home, map[string]string{AutoConfigEnvVar: "true"})
	r.NoError(AutoConfigure(cmd))

	_, err := os.Stat(filepath.Join(home, configuration.OCMConfigFileName))
	r.True(os.IsNotExist(err))
}

func TestAutoConfigure_SkipsWhenConfigFlagSet(t *testing.T) {
	r := require.New(t)
	home := t.TempDir()
	writeDockerConfig(t, filepath.Join(home, ".docker"))

	cmd := newTestCommand(t, home, map[string]string{})
	r.NoError(cmd.PersistentFlags().Set(configuration.OCMConfigCommandArgument, "/some/config.yaml"))

	r.NoError(AutoConfigure(cmd))

	_, err := os.Stat(filepath.Join(home, configuration.OCMConfigFileName))
	r.True(os.IsNotExist(err))
}
