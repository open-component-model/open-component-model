package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/spf13/cobra"

	"ocm.software/open-component-model/bindings/go/credentials"
	credentialsRuntime "ocm.software/open-component-model/bindings/go/credentials/spec/config/runtime"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/cmd/configuration"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
	credentialsConfig "ocm.software/open-component-model/cli/internal/credentials"
	"ocm.software/open-component-model/cli/internal/plugin/builtin"
	builtinConfig "ocm.software/open-component-model/cli/internal/plugin/builtin/config"
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

func setupPluginManager(cmd *cobra.Command) error {
	pluginManager := manager.NewPluginManager(cmd.Context())

	// Get base configuration and create merged config with CLI flag overrides
	baseCfg := ocmctx.FromContext(cmd.Context()).Configuration()
	mergedCfg, err := builtinConfig.GetMergedConfigWithCLIFlags(cmd, baseCfg)
	if err != nil {
		return fmt.Errorf("could not get merged configuration with CLI flags: %w", err)
	}

	// Use merged configuration for external plugins (if any exist)
	if mergedCfg != nil {
		pluginCfg, err := v2alpha1.LookupConfig(mergedCfg)
		if err != nil {
			return fmt.Errorf("could not get plugin configuration: %w", err)
		}
		for _, pluginLocation := range pluginCfg.Locations {
			err := pluginManager.RegisterPlugins(cmd.Context(), pluginLocation,
				manager.WithIdleTimeout(time.Duration(pluginCfg.IdleTimeout)),
				manager.WithConfiguration(mergedCfg), // Use merged config
			)
			if errors.Is(err, manager.ErrNoPluginsFound) {
				slog.DebugContext(cmd.Context(), "no plugins found at location", slog.String("location", pluginLocation))
				continue
			}
			if err != nil {
				return err
			}
		}
	} else {
		slog.WarnContext(cmd.Context(), "could not get configuration to initialize plugin manager")
	}

	builtinPluginConfig, err := builtinConfig.GetMergedBuiltinPluginConfig(cmd, baseCfg)
	if err != nil {
		return fmt.Errorf("could not get built-in plugin configuration: %w", err)
	}

	builtinLogger, err := builtinConfig.GetBuiltinPluginLogger(cmd)
	if err != nil {
		return fmt.Errorf("could not get built-in plugin logger: %w", err)
	}

	if err := builtin.Register(pluginManager, builtinPluginConfig, builtinLogger); err != nil {
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
