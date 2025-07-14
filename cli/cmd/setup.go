package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/spf13/cobra"

	"ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1"
	v1 "ocm.software/open-component-model/bindings/go/configuration/v1"
	"ocm.software/open-component-model/bindings/go/credentials"
	credentialsRuntime "ocm.software/open-component-model/bindings/go/credentials/spec/config/runtime"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/cmd/configuration"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
	credentialsConfig "ocm.software/open-component-model/cli/internal/credentials"
	"ocm.software/open-component-model/cli/internal/plugin/builtin"
	"ocm.software/open-component-model/cli/internal/plugin/spec/config/v2alpha1"
)

func setupOCMConfig(cmd *cobra.Command) {
	if cfg, err := configuration.GetFlattenedOCMConfigForCommand(cmd); err != nil {
		slog.DebugContext(cmd.Context(), "could not get configuration", slog.String("error", err.Error()))
	} else {
		ctx := ocmctx.WithConfiguration(cmd.Context(), cfg)
		cmd.SetContext(ctx)
	}
}

func setupFilesystemConfig(cmd *cobra.Command) {
	value, _ := cmd.PersistentFlags().GetString(tempFolderFlag)
	ocmCtx := ocmctx.FromContext(cmd.Context())
	cfg := ocmCtx.Configuration()

	var fsCfg *v1alpha1.Config
	var err error

	if cfg == nil {
		slog.WarnContext(cmd.Context(), "could not get configuration to initialize filesystem config")
		fsCfg = &v1alpha1.Config{}
	} else {
		fsCfg, err = v1alpha1.LookupConfig(cfg)
		if err != nil {
			slog.DebugContext(cmd.Context(), "could not get filesystem configuration", slog.String("error", err.Error()))
			fsCfg = &v1alpha1.Config{}
		}
	}

	// CLI flag takes precedence over the config file
	if value != "" {
		fsCfg.TempFolder = value

		// If we have a CLI flag but no filesystem config in the config,
		// we need to add it to the configuration
		if cfg != nil && !hasFilesystemConfig(cfg) {
			if err := addFilesystemConfigToCentralConfig(cmd, fsCfg); err != nil {
				slog.WarnContext(cmd.Context(), "could not add filesystem config to central configuration", slog.String("error", err.Error()))
			}
		}
	}

	ctx := ocmctx.WithFilesystemConfig(cmd.Context(), fsCfg)
	cmd.SetContext(ctx)
}

// hasFilesystemConfig checks if the central configuration already contains filesystem configuration
func hasFilesystemConfig(cfg *v1.Config) bool {
	if cfg == nil {
		return false
	}
	for _, configEntry := range cfg.Configurations {
		if configEntry.Name == v1alpha1.ConfigType {
			return true
		}
	}

	return false
}

// addFilesystemConfigToCentralConfig adds the filesystem configuration to the central configuration
func addFilesystemConfigToCentralConfig(cmd *cobra.Command, fsCfg *v1alpha1.Config) error {
	ocmCtx := ocmctx.FromContext(cmd.Context())
	cfg := ocmCtx.Configuration()
	if cfg == nil {
		return fmt.Errorf("no central configuration available")
	}

	raw := &runtime.Raw{}
	if err := v1.Scheme.Convert(fsCfg, raw); err != nil {
		return fmt.Errorf("failed to convert filesystem config to raw: %w", err)
	}
	cfg.Configurations = append(cfg.Configurations, raw)

	ctx := ocmctx.WithConfiguration(cmd.Context(), cfg)
	cmd.SetContext(ctx)

	return nil
}

func setupPluginManager(cmd *cobra.Command) error {
	pluginManager := manager.NewPluginManager(cmd.Context())

	if cfg := ocmctx.FromContext(cmd.Context()).Configuration(); cfg == nil {
		slog.WarnContext(cmd.Context(), "could not get configuration to initialize plugin manager")
	} else {
		pluginCfg, err := v2alpha1.LookupConfig(cfg)
		if err != nil {
			return fmt.Errorf("could not get plugin configuration: %w", err)
		}
		for _, pluginLocation := range pluginCfg.Locations {
			err := pluginManager.RegisterPlugins(cmd.Context(), pluginLocation,
				manager.WithIdleTimeout(time.Duration(pluginCfg.IdleTimeout)),
			)
			if errors.Is(err, manager.ErrNoPluginsFound) {
				slog.DebugContext(cmd.Context(), "no plugins found at location", slog.String("location", pluginLocation))
				continue
			}
			if err != nil {
				return err
			}
		}
	}

	ocmContext := ocmctx.FromContext(cmd.Context())
	filesystemConfig := ocmContext.FilesystemConfig()
	if err := builtin.Register(pluginManager, filesystemConfig); err != nil {
		return fmt.Errorf("could not register builtin plugins: %w", err)
	}

	ctx := ocmctx.WithPluginManager(cmd.Context(), pluginManager)
	cmd.SetContext(ctx)

	return nil
}

func setupCredentialGraph(cmd *cobra.Command) error {
	pluginManager := ocmctx.FromContext(cmd.Context()).PluginManager()
	if pluginManager == nil {
		return fmt.Errorf("could not get plugin manager to initialize credential graph")
	}

	opts := credentials.Options{
		RepositoryPluginProvider: pluginManager.CredentialRepositoryRegistry,
		CredentialPluginProvider: credentials.GetCredentialPluginFn(
			// TODO(jakobmoellerdev): use the plugin manager to get the credential plugin once we have some.
			func(ctx context.Context, typed runtime.Typed) (credentials.CredentialPlugin, error) {
				return nil, fmt.Errorf("no credential plugin found for type %s", typed)
			},
		),
		CredentialRepositoryTypeScheme: pluginManager.CredentialRepositoryRegistry.RepositoryScheme(),
	}

	var credCfg *credentialsRuntime.Config
	var err error
	if cfg := ocmctx.FromContext(cmd.Context()).Configuration(); cfg == nil {
		slog.WarnContext(cmd.Context(), "could not get configuration to initialize credential graph")
		credCfg = &credentialsRuntime.Config{}
	} else if credCfg, err = credentialsConfig.LookupCredentialConfiguration(cfg); err != nil {
		return fmt.Errorf("could not get credential configuration: %w", err)
	}

	graph, err := credentials.ToGraph(cmd.Context(), credCfg, opts)
	if err != nil {
		return fmt.Errorf("could not create credential graph: %w", err)
	}

	cmd.SetContext(ocmctx.WithCredentialGraph(cmd.Context(), graph))

	return nil
}
