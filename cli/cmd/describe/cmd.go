package describe

import (
	"github.com/spf13/cobra"

	"ocm.software/open-component-model/cli/cmd/describe/types"
)

// New represents any command that is related to describing objects or metadata.
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "describe",
		Short: "Describe OCM entities or metadata",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(types.New())
	return cmd
}
