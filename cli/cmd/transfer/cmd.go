package transfer

import (
	"github.com/spf13/cobra"

	componentversion "ocm.software/open-component-model/cli/cmd/transfer/component-version"
)

// New represents any command that is related to "transfer"ing objects
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "transfer {component-version|component-versions|cv|cvs}",
		Short: "Transfer anything in OCM",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(componentversion.New())
	return cmd
}
