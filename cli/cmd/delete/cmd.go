package deletecmd

import (
	"github.com/spf13/cobra"

	componentversion "ocm.software/open-component-model/cli/cmd/delete/component-version"
)

// New represents any command that is related to deleting objects
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete {component-version|cv}",
		Short: "Delete objects from OCM",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(componentversion.New())
	return cmd
}
