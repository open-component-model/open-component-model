package config

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"sigs.k8s.io/yaml"

	configv1 "ocm.software/open-component-model/bindings/go/configuration/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/internal/flags/enum"
	"ocm.software/open-component-model/cli/internal/flags/log"
	builtinv1 "ocm.software/open-component-model/cli/internal/plugin/builtin/config/v1"
)

// AddBuiltinPluginFlags adds CLI flags for built-in plugin configuration.
func AddBuiltinPluginFlags(flags *pflag.FlagSet) {
	flags.String("temp-folder", "", "Temporary folder location for the library and plugins.")
}

// GetMergedBuiltinPluginConfig creates builtin plugin configuration by merging config file and CLI flags.
// CLI flags take precedence over configuration file settings.
// The returned config includes log settings from the config file that can be overridden by existing CLI log flags.
func GetMergedBuiltinPluginConfig(cmd *cobra.Command, config *configv1.Config) (*builtinv1.BuiltinPluginConfig, error) {
	builtinConfig, err := builtinv1.LookupBuiltinPluginConfig(config)
	if err != nil {
		return nil, err
	}

	// Override log settings with CLI flags if they are set
	if cmd.Flags().Changed(log.LevelFlagName) {
		if level, err := enum.Get(cmd.Flags(), log.LevelFlagName); err == nil {
			builtinConfig.LogLevel = builtinv1.LogLevel(level)
		}
	}

	if cmd.Flags().Changed(log.FormatFlagName) {
		if format, err := enum.Get(cmd.Flags(), log.FormatFlagName); err == nil {
			builtinConfig.LogFormat = builtinv1.LogFormat(format)
		}
	}

	if cmd.Flags().Changed("temp-folder") {
		if folder, err := cmd.Flags().GetString("temp-folder"); err == nil {
			builtinConfig.TempFolder = folder
		}
	}

	return builtinConfig, nil
}

// GetBuiltinPluginLogger creates a logger for built-in plugins using the existing log infrastructure.
// This ensures consistent logging configuration across the entire CLI.
func GetBuiltinPluginLogger(cmd *cobra.Command) (*slog.Logger, error) {
	return log.GetBaseLogger(cmd)
}

// GetMergedConfigWithCLIFlags creates a new global configuration that includes CLI flag overrides.
// This ensures both external and built-in plugins receive the same merged configuration.
func GetMergedConfigWithCLIFlags(cmd *cobra.Command, baseConfig *configv1.Config) (*configv1.Config, error) {
	if baseConfig == nil {
		return nil, nil
	}

	if !cmd.Flags().Changed(log.LevelFlagName) && !cmd.Flags().Changed(log.FormatFlagName) && !cmd.Flags().Changed("temp-folder") {
		return baseConfig, nil
	}

	mergedBuiltinPluginConfig, err := GetMergedBuiltinPluginConfig(cmd, baseConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to get built-in plugin configuration: %w", err)
	}

	// ensure we change a new config
	mergedConfig := baseConfig.DeepCopy()

	found := false
	for i, cfg := range mergedConfig.Configurations {
		// TODO: We only deal with one right now
		if cfg.Type.String() == builtinv1.ConfigType {
			// We found the existing config, encode it back and add it to the data.
			encoded, err := yaml.Marshal(mergedBuiltinPluginConfig)
			if err != nil {
				return nil, fmt.Errorf("failed to encode merged built-in plugin configuration: %w", err)
			}
			mergedConfig.Configurations[i].Data = encoded

			// we found a config all good
			found = true
			break
		}
	}

	// If no built-in config was found in the original config, but we have CLI overrides,
	// add the built-in config to the merged config
	if !found {
		encoded, err := yaml.Marshal(mergedBuiltinPluginConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to encode built-in plugin configuration: %w", err)
		}

		mergedConfig.Configurations = append(mergedConfig.Configurations, &runtime.Raw{
			Type: mergedBuiltinPluginConfig.GetType(),
			Data: encoded,
		})
	}

	return mergedConfig, nil
}
