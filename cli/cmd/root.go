package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/spf13/cobra"

	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/configuration/v1"
	credentialsConfig "ocm.software/open-component-model/cli/internal/credentials"
	ocicredentialplugin "ocm.software/open-component-model/cli/internal/plugin/credentials/oci"
	ctfplugin "ocm.software/open-component-model/cli/internal/plugin/ctf"
	ociplugin "ocm.software/open-component-model/cli/internal/plugin/oci"
	"ocm.software/open-component-model/cli/internal/plugin/spec/config/v2alpha1"
	"ocm.software/open-component-model/cli/log"
)

type OCM struct {
	*cobra.Command             // the root command
	Configuration   *v1.Config // the global ocm configuration
	PluginManager   *manager.PluginManager
	CredentialGraph *credentials.Graph
}

// Root represents the base command when called without any subcommands
var Root *OCM

func init() {
	Root = &OCM{
		Command: &cobra.Command{
			Use:   "ocm [sub-command]",
			Short: "The official Open Component Model (OCM) CLI",
			Long: `The Open Component Model command line client supports the work with OCM
  artifacts, like Component Archives, Common Transport Archive,
  Component Repositories, and Component Versions.`,
			RunE: func(cmd *cobra.Command, args []string) error {
				return cmd.Help()
			},
			PersistentPreRunE: setupRoot,
			DisableAutoGenTag: true,
		},
	}

	v1.RegisterConfigFlag(Root.Command)
	log.RegisterLoggingFlags(Root.Command.PersistentFlags())
	Root.AddCommand(GenerateCmd)
	Root.AddCommand(GetCmd)
}

// setupRoot sets up the root command with the necessary setup for all cli commands.
func setupRoot(cmd *cobra.Command, _ []string) error {
	logger, err := log.GetBaseLogger(cmd)
	if err != nil {
		return fmt.Errorf("could not retrieve logger: %w", err)
	}
	slog.SetDefault(logger)

	if cfg, err := v1.GetFlattenedOCMConfigForCommand(cmd); err != nil {
		logger.Debug("could not get configuration", slog.String("error", err.Error()))
	} else {
		Root.Configuration = cfg
	}

	if err := setupPluginManager(cmd, err); err != nil {
		return fmt.Errorf("could not setup plugin manager: %w", err)
	}

	if err := setupCredentialGraph(cmd); err != nil {
		return fmt.Errorf("could not setup credential graph: %w", err)
	}

	return nil
}

func setupPluginManager(cmd *cobra.Command, err error) error {
	Root.PluginManager = manager.NewPluginManager(cmd.Context())
	pluginCfg, err := v2alpha1.LookupConfig(Root.Configuration)
	if err != nil {
		return fmt.Errorf("could not get plugin configuration: %w", err)
	}
	for _, pluginLocation := range pluginCfg.Locations {
		if err := Root.PluginManager.RegisterPlugins(cmd.Context(), pluginLocation,
			manager.WithIdleTimeout(time.Duration(pluginCfg.IdleTimeout)),
		); err != nil {
			slog.WarnContext(cmd.Context(), "could not register plugin location", "error", err)
		}
	}

	if err := ocicredentialplugin.Register(Root.PluginManager.CredentialRepositoryRegistry); err != nil {
		return fmt.Errorf("could not register OCI credential plugin: %w", err)
	}

	if err := ociplugin.Register(Root.PluginManager.ComponentVersionRepositoryRegistry); err != nil {
		return fmt.Errorf("could not register OCI plugin: %w", err)
	}

	if err := ctfplugin.Register(Root.PluginManager.ComponentVersionRepositoryRegistry); err != nil {
		return fmt.Errorf("could not register CTF plugin: %w", err)
	}

	return nil
}

func setupCredentialGraph(cmd *cobra.Command) error {
	if credCfg, err := credentialsConfig.LookupCredentialConfiguration(Root.Configuration); err != nil {
		return fmt.Errorf("could not get credential configuration: %w", err)
	} else {
		graph, err := credentials.ToGraph(cmd.Context(), credCfg, credentials.Options{
			RepositoryPluginProvider: Root.PluginManager.CredentialRepositoryRegistry,
			CredentialPluginProvider: credentials.GetCredentialPluginFn(func(ctx context.Context, typed runtime.Typed) (credentials.CredentialPlugin, error) {
				return nil, fmt.Errorf("no credential plugin found for type %s", typed)
			}),
			CredentialRepositoryTypeScheme: Root.PluginManager.CredentialRepositoryRegistry.RepositoryScheme(),
		})
		if err != nil {
			return fmt.Errorf("could not create credential graph: %w", err)
		}
		Root.CredentialGraph = graph
	}
	return nil
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the Root.
func Execute() {
	err := Root.Execute()
	if err != nil {
		os.Exit(1)
	}
}
