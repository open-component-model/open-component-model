package cmd

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"ocm.software/open-component-model/cli/cmd/generate"
	"ocm.software/open-component-model/cli/cmd/get"
	"ocm.software/open-component-model/cli/configuration/v1"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
	"ocm.software/open-component-model/cli/log"
)

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the root.
func Execute() {
	err := root.Execute()
	if err != nil {
		os.Exit(1)
	}
}

// root represents the base command when called without any subcommands
var root *cobra.Command

func init() {
	root = &cobra.Command{
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
	}

	v1.RegisterConfigFlag(root)
	log.RegisterLoggingFlags(root.PersistentFlags())
	root.AddCommand(generate.Cmd)
	root.AddCommand(get.Cmd)
}

// setupRoot sets up the root command with the necessary setup for all cli commands.
func setupRoot(cmd *cobra.Command, _ []string) error {
	logger, err := log.GetBaseLogger(cmd)
	if err != nil {
		return fmt.Errorf("could not retrieve logger: %w", err)
	}
	slog.SetDefault(logger)

	setupOCMConfig(cmd)

	if err := setupPluginManager(cmd); err != nil {
		return fmt.Errorf("could not setup plugin manager: %w", err)
	}

	if err := setupCredentialGraph(cmd); err != nil {
		return fmt.Errorf("could not setup credential graph: %w", err)
	}

	ocmctx.RegisterAtRoot(cmd)

	return nil
}
