package plugins

import (
	"github.com/spf13/cobra"

	"ocm.software/open-component-model/cli/cmd/plugins/get"
	"ocm.software/open-component-model/cli/cmd/plugins/list"
)

// New represents any command that is related to adding ( "add"ing ) objects
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plugin",
		Short: "Manage OCM plugins",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	registry := &cobra.Command{
		Use:   "registry {get|list}",
		Short: "Manage plugin registries",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	registry.AddCommand(get.New())
	registry.AddCommand(list.New())

	cmd.AddCommand(registry)

	return cmd
}
