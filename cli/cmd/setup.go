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

func setupTempDir(cmd *cobra.Command) error {
	tempDir, err := cmd.Flags().GetString("temp-dir")
	if err != nil {
		return fmt.Errorf("could not get temp-dir flag: %w", err)
	}

	ctx := ocmctx.WithTempDir(cmd.Context(), tempDir)
	// TODO: set up the oci provider context here as well.
	cmd.SetContext(ctx)

	return nil
}

func setupPluginManager(cmd *cobra.Command) error {
	// TODO: this will passed to `NewPluginManager`
	// tempDir := ocmctx.FromContext(cmd.Context()).TempDir()
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

	if err := builtin.Register(pluginManager); err != nil {
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
