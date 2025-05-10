package cmd

import (
	"github.com/spf13/cobra"
)

// GetCmd represents any command that is related to retrieving ( "get"ting ) objects
var GetCmd = &cobra.Command{
	Use:   "get {component-version|component-versions|cv|cvs}",
	Short: "Get anything from OCM",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}
