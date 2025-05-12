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

// Execute adds all child commands to the Cmd command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the Cmd.
func Execute() {
	err := New().Execute()
	if err != nil {
		os.Exit(1)
	}
}

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ocm [sub-command]",
		Short: "The official Open Component Model (OCM) CLI",
		Long: `The Open Component Model command line client supports the work with OCM
  artifacts, like Component Archives, Common Transport Archive,
  Component Repositories, and Component Versions.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
		PersistentPreRunE: preRunE,
		DisableAutoGenTag: true,
	}

	v1.RegisterConfigFlag(cmd)
	log.RegisterLoggingFlags(cmd.PersistentFlags())
	cmd.AddCommand(generate.New())
	cmd.AddCommand(get.New())
	return cmd
}

// preRunE sets up the Cmd command with the necessary setup for all cli commands.
func preRunE(cmd *cobra.Command, _ []string) error {
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

	if parent := cmd.Parent(); parent != nil {
		cmd.SetOut(parent.OutOrStdout())
		cmd.SetErr(parent.ErrOrStderr())
	}

	return nil
}
