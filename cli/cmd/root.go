package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"ocm.software/open-component-model/bindings/go/runtime"
	v1 "ocm.software/open-component-model/cli/configuration/v1"
	"ocm.software/open-component-model/cli/log"
)

type OCM struct {
	*cobra.Command                        // the root command
	Configuration      *v1.Config         // the global ocm configuration
	CredentialResolver CredentialResolver // the credential graph
}

type CredentialResolver interface {
	Resolve(ctx context.Context, identity runtime.Identity) (map[string]string, error)
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
			// Uncomment the following line if your bare application
			// has an action associated with it:
			RunE: func(cmd *cobra.Command, args []string) error {
				return cmd.Help()
			},
			PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
				logger, err := log.GetBaseLogger(cmd)
				if err != nil {
					return fmt.Errorf("could not retrieve logger: %w", err)
				}
				slog.SetDefault(logger)

				cfg, err := v1.GetFlattenedOCMConfigForCommand(cmd)
				if err != nil {
					return fmt.Errorf("could not retrieve OCM configuration: %w", err)
				}
				Root.Configuration = cfg

				return nil
			},
			DisableAutoGenTag: true,
		},
	}

	Root.AddCommand(GenerateCmd)
	v1.RegisterConfigFlag(Root.Command)
	log.RegisterLoggingFlags(Root.Command)
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the Root.
func Execute() {
	err := Root.Execute()
	if err != nil {
		os.Exit(1)
	}
}
