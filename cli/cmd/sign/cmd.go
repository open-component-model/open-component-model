package sign

import (
	"github.com/spf13/cobra"

	componentversion "ocm.software/open-component-model/cli/cmd/sign/component-version"
)

// New represents any command that is related to signing objects
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sign {component-version|component-versions|cv|cvs}",
		Short: "create signatures for component versions in OCM",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(componentversion.New())
	return cmd
}
