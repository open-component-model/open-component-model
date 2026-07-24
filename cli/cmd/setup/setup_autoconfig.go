package setup

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	credv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	ocicredsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/cmd/configuration"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
)

const (
	// AutoConfigEnvVar disables the first-startup auto configuration when set to a truthy value.
	AutoConfigEnvVar = "OCM_DISABLE_AUTO_CONFIG"
	// dockerConfigEnvVar is the standard docker environment variable pointing at the docker config directory.
	dockerConfigEnvVar   = "DOCKER_CONFIG"
	dockerConfigDirName  = ".docker"
	dockerConfigFileName = "config.json"
	xdgConfigHomeEnvVar  = "XDG_CONFIG_HOME"
)

// AutoConfigure performs a one-time, best-effort setup of an OCM configuration on first startup.
//
// It only runs when no OCM configuration exists in any of the well known locations and the user
// did not supply an explicit --config flag. If a docker configuration file is detected (via
// $DOCKER_CONFIG or $HOME/.docker/config.json), it writes an initial OCM config that wires the
// docker config in as a credential repository, so docker credentials are reused transparently.
//
// The generated configuration is written to the default OCM config location where the regular
// configuration lookup will pick it up on this and subsequent runs.
func AutoConfigure(cmd *cobra.Command) error {
	ctx := cmd.Context()

	// Respect an explicit --config flag: the user is managing configuration manually.
	if flag := cmd.Flag(configuration.OCMConfigCommandArgument); flag != nil && flag.Changed {
		return nil
	}

	syscalls := ocmctx.FromContext(ctx).Syscalls()
	if syscalls == nil {
		return nil
	}

	if autoConfigDisabled(syscalls.Getenv(AutoConfigEnvVar)) {
		slog.DebugContext(ctx, "auto configuration disabled via environment variable")
		return nil
	}

	opts := configuration.OCMConfigOptions{
		Stat:        syscalls.Stat,
		Getenv:      syscalls.Getenv,
		UserHomeDir: syscalls.UserHomeDir,
		Getwd:       syscalls.Getwd,
		Executable:  syscalls.Executable,
	}

	// Only run on first startup: skip if any OCM config already exists.
	if paths, err := configuration.GetOCMConfigPaths(opts); err == nil && len(paths) > 0 {
		slog.DebugContext(ctx, "ocm configuration already present, skipping auto configuration", slog.Any("paths", paths))
		return nil
	}

	dockerConfigPath, ok := detectDockerConfig(syscalls)
	if !ok {
		slog.DebugContext(ctx, "no docker config detected, skipping auto configuration")
		return nil
	}

	target, err := defaultConfigWritePath(syscalls)
	if err != nil {
		return fmt.Errorf("could not determine ocm config write location: %w", err)
	}

	cfg, err := buildDockerConfigOCMConfig(dockerConfigPath)
	if err != nil {
		return fmt.Errorf("could not build ocm configuration: %w", err)
	}

	if err := writeConfig(target, cfg); err != nil {
		return fmt.Errorf("could not write ocm configuration to %q: %w", target, err)
	}

	slog.InfoContext(ctx, "created initial ocm configuration from detected docker config",
		slog.String("config", target),
		slog.String("dockerConfig", dockerConfigPath),
	)

	return nil
}

// detectDockerConfig locates a docker config.json using the standard docker resolution order:
// $DOCKER_CONFIG/config.json first, then $HOME/.docker/config.json.
func detectDockerConfig(s *ocmctx.Syscalls) (string, bool) {
	var candidate string
	if dir := s.Getenv(dockerConfigEnvVar); dir != "" {
		candidate = filepath.Join(dir, dockerConfigFileName)
	} else if home, err := s.UserHomeDir(); err == nil {
		candidate = filepath.Join(home, dockerConfigDirName, dockerConfigFileName)
	}
	if candidate == "" {
		return "", false
	}
	if info, err := s.Stat(candidate); err != nil || info.IsDir() {
		return "", false
	}
	return candidate, true
}

// defaultConfigWritePath returns the location the generated config is written to. It mirrors the
// discovery logic so the file is found on subsequent runs: $XDG_CONFIG_HOME/.ocm/config if
// XDG_CONFIG_HOME is set, otherwise $HOME/.ocm/config.
func defaultConfigWritePath(s *ocmctx.Syscalls) (string, error) {
	if xdg := s.Getenv(xdgConfigHomeEnvVar); xdg != "" {
		return filepath.Join(xdg, configuration.OCMConfigFileName), nil
	}
	home, err := s.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, configuration.OCMConfigFileName), nil
}

// buildDockerConfigOCMConfig assembles a generic OCM configuration that embeds a credentials
// configuration referencing the given docker config file as a credential repository.
func buildDockerConfigOCMConfig(dockerConfigPath string) (*genericv1.Config, error) {
	dockerRaw, err := toRaw(&ocicredsv1.DockerConfig{
		Type:             ocicredsv1.DockerConfigVersionedType,
		DockerConfigFile: dockerConfigPath,
	})
	if err != nil {
		return nil, fmt.Errorf("could not encode docker config credential repository: %w", err)
	}

	credRaw, err := toRaw(&credv1.Config{
		Type:         runtime.NewVersionedType(credv1.ConfigType, credv1.Version),
		Repositories: []credv1.RepositoryConfigEntry{{Repository: dockerRaw}},
	})
	if err != nil {
		return nil, fmt.Errorf("could not encode credentials configuration: %w", err)
	}

	return &genericv1.Config{
		Type:           runtime.NewVersionedType(genericv1.ConfigType, genericv1.ConfigTypeV1),
		Configurations: []*runtime.Raw{credRaw},
	}, nil
}

// toRaw converts a typed object into a runtime.Raw by round-tripping through JSON.
func toRaw(obj runtime.Typed) (*runtime.Raw, error) {
	data, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	raw := &runtime.Raw{}
	if err := raw.UnmarshalJSON(data); err != nil {
		return nil, err
	}
	return raw, nil
}

// writeConfig serializes the configuration as YAML and writes it to path, creating parent
// directories as needed.
func writeConfig(path string, cfg *genericv1.Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("could not marshal configuration: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("could not create config directory: %w", err)
	}
	return os.WriteFile(path, data, 0o600)
}

// autoConfigDisabled parses a boolean opt-out value. Unrecognized non-empty values are treated
// as "disabled" for safety.
func autoConfigDisabled(val string) bool {
	if val == "" {
		return false
	}
	disabled, err := strconv.ParseBool(val)
	if err != nil {
		return true
	}
	return disabled
}
