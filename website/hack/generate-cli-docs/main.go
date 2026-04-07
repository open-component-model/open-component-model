package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"

	clictx "ocm.software/ocm/api/cli"
	"ocm.software/ocm/cmds/ocm/app"
)

// Lists commands that should be excluded from generated docs.
var commandDenyList = []string{
	"bootstrap",
	"cache",
	"completion",
	"controller",
	"credentials",
	"execute",
	"hash",
	"help",
	"install",
	"oci",
	"ocm",
	"toi",
	"version",
}

// Parses flags and runs the generator.
func main() {
	var outputDir string

	flag.StringVar(&outputDir, "output-dir", "./content/docs/reference/ocm-cli", "output directory for generated docs")
	flag.Parse()

	if outputDir == "" {
		log.Fatal("Flag -output-dir is required")
	}

	if err := run(outputDir); err != nil {
		log.Fatal(err)
	}
}

// Generates CLI docs into the provided output directory.
func run(dir string) error {
	log.Println("Generating docs for OCM CLI")

	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("error clearing output directory: %w", err)
	}

	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return fmt.Errorf("error creating output directory: %w", err)
	}

	cmd := app.NewCliCommand(clictx.DefaultContext())

	for _, subCmd := range cmd.Commands() {
		if slices.Contains(commandDenyList, subCmd.Name()) {
			cmd.RemoveCommand(subCmd)
		}
	}

	cmdToLink := buildCmdToLink("docs/reference/ocm-cli")
	if err := genMarkdownTreeCustom(cmd, dir, "ocm-cli", cmdToLink); err != nil {
		return fmt.Errorf("error generating markdown: %w", err)
	}

	if err := genIndexForRootHelpTopics(filepath.Join(dir, "help")); err != nil {
		return fmt.Errorf("error generating ocm index: %w", err)
	}

	log.Printf("Docs successfully written to %s\n", dir)

	return nil
}
