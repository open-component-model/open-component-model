package skill

import (
	"github.com/spf13/cobra"

	"ocm.software/open-component-model/cli/cmd/skill/pull"
	"ocm.software/open-component-model/cli/cmd/skill/push"
)

// New returns the root "skill" command grouping pull and push subcommands.
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill {pull|push}",
		Short: "Manage AI skills distributed via OCM component catalogues",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
		DisableAutoGenTag: true,
	}
	cmd.AddCommand(pull.New())
	cmd.AddCommand(push.New())
	return cmd
}
