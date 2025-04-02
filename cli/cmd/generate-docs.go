package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"

	"ocm.software/open-component-model/cli/internal/enum"
)

const (
	FlagDirectory          = "directory"
	FlagDirectoryShortHand = "d"

	FlagMode = "mode"
)

const (
	GenerationModeMarkdown     = "markdown"
	GenerationModeReStructured = "restructured"
	GenerationModeMan          = "man"
	GenerationModeYAML         = "yaml"
)

// GenerateDocsCmd represents the docs command
var GenerateDocsCmd = &cobra.Command{
	Use:   "docs [-d <directory>]",
	Short: "Generation Documentation for the CLI",
	Long:  ``,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := cmd.Flags().GetString(FlagDirectory)
		if err != nil {
			return err
		}
		if dir == "" {
			if dir, err = os.Getwd(); err != nil {
				return err
			}
		}
		if err := os.MkdirAll(dir, os.ModePerm); err != nil {
			return err
		}

		mode, err := enum.Get(cmd.Flags(), FlagMode)
		if err != nil {
			return err
		}

		switch mode {
		case GenerationModeMarkdown:
			return doc.GenMarkdownTree(RootCmd, dir)
		case GenerationModeReStructured:
			return doc.GenReSTTree(RootCmd, dir)
		case GenerationModeMan:
			return doc.GenManTree(RootCmd, &doc.GenManHeader{
				Source: "Auto generated by OCM CLI powered by spf13/cobra",
			}, dir)
		case GenerationModeYAML:
			return doc.GenYamlTree(RootCmd, dir)
		}

		return fmt.Errorf("unknown generation mode: %s", mode)
	},
}

func init() {
	GenerateCmd.AddCommand(GenerateDocsCmd)

	GenerateDocsCmd.Flags().StringP(FlagDirectory, FlagDirectoryShortHand, "", "directory to generate docs to. If not set, current working directory is used.")
	enum.Var(GenerateDocsCmd.Flags(), FlagMode, []string{GenerationModeMarkdown, GenerationModeReStructured, GenerationModeMan}, "generation mode to use")
}
