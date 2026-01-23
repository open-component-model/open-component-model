package setup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/credentials"
	credentialsRuntime "ocm.software/open-component-model/bindings/go/credentials/spec/config/runtime"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/cmd/configuration"
	ocmcmd "ocm.software/open-component-model/cli/cmd/internal/cmd"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
	"ocm.software/open-component-model/cli/internal/plugin/builtin"
	"ocm.software/open-component-model/cli/internal/plugin/spec/config/v2alpha1"
)

func OCMConfig(cmd *cobra.Command) {
	cfg, err := configuration.GetFlattenedOCMConfigForCommand(cmd)
	if err != nil {
		slog.DebugContext(cmd.Context(), "could not get configuration", slog.String("error", err.Error()))
		cfg = &genericv1.Config{}
	}

	ctx := ocmctx.WithConfiguration(cmd.Context(), cfg)
	cmd.SetContext(ctx)
}

func PluginManager(cmd *cobra.Command) error {
	pluginManager := manager.NewPluginManager(cmd.Context())

	if cfg := ocmctx.FromContext(cmd.Context()).Configuration(); cfg == nil {
		slog.WarnContext(cmd.Context(), "could not get configuration to initialize plugin manager")
	} else {
		pluginCfg, err := v2alpha1.LookupConfig(cfg)
		if err != nil {
			return fmt.Errorf("could not get plugin configuration: %w", err)
		}

		if defaultDir, err := cmd.Flags().GetString(ocmcmd.PluginDirectoryFlag); err == nil {
			expanded := os.ExpandEnv(defaultDir)
			pluginCfg.Locations = []string{expanded}
		}

		if pluginCfg.IdleTimeout == 0 {
			pluginCfg.IdleTimeout = v2alpha1.Duration(time.Hour)
		}

		var loadedAnyPlugins bool
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

			loadedAnyPlugins = true
		}

		if !loadedAnyPlugins {
			slog.DebugContext(cmd.Context(), "no plugins found at any of the configured locations", slog.String("locations", strings.Join(pluginCfg.Locations, ", ")))
		}
	}

	ocmContext := ocmctx.FromContext(cmd.Context())
	filesystemConfig := ocmContext.FilesystemConfig()
	if err := builtin.Register(pluginManager, filesystemConfig, slog.Default()); err != nil {
		return fmt.Errorf("could not register builtin plugins: %w", err)
	}

	ctx := ocmctx.WithPluginManager(cmd.Context(), pluginManager)
	cmd.SetContext(ctx)

	cobra.OnFinalize(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := pluginManager.Shutdown(shutdownCtx); err != nil {
			slog.ErrorContext(shutdownCtx, "failed to shutdown plugin manager", slog.String("error", err.Error()))
		}
	})

	return nil
}

func CredentialGraph(cmd *cobra.Command) error {
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
	} else if credCfg, err = credentialsRuntime.ExtractCredentialConfig(cfg); err != nil {
		return fmt.Errorf("could not get credential configuration: %w", err)
	}

	if credCfg == nil {
		credCfg = &credentialsRuntime.Config{}
	}

	graph, err := credentials.ToGraph(cmd.Context(), credCfg, opts)
	if err != nil {
		return fmt.Errorf("could not create credential graph: %w", err)
	}

	cmd.SetContext(ocmctx.WithCredentialGraph(cmd.Context(), graph))

	return nil
}
