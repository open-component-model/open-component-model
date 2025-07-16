package download

import (
	"github.com/spf13/cobra"

	"ocm.software/open-component-model/cli/cmd/download/resource"
)

// New represents any command that is related to adding ( "add"ing ) objects
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "download {resource|resources}",
		Short: "Download anything from OCM",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(resource.New())
	return cmd
}
