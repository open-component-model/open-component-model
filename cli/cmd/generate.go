package cmd

import (
	"github.com/spf13/cobra"
)

// GenerateCmd represents the generate command
var GenerateCmd = &cobra.Command{
	Use:   "generate {docs}",
	Short: "Generate documentation for the OCM CLI",
	Long: `A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
}
