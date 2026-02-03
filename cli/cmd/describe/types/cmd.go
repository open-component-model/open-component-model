package types

import (
	"fmt"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"

	"ocm.software/open-component-model/bindings/go/cel/expression/fieldpath"
	"ocm.software/open-component-model/bindings/go/runtime"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
	"ocm.software/open-component-model/cli/internal/flags/enum"
	"ocm.software/open-component-model/cli/internal/subsystem"
)

// New represents the command to describe OCM types.
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "types [subsystem [type [field-path]]]",
		Aliases: []string{"type"},
		Short:   "Describe OCM types and their configuration schema",
		Long: `Describe OCM types registered in various subsystems.

WHAT ARE SUBSYSTEMS?
  OCM uses a plugin-based architecture where different types of functionality are organized
  into subsystems. Each subsystem is a collection of related type implementations. When you
  use OCM commands or configure OCM resources, you specify types from these subsystems.

  Common subsystems:
    - ocm-repository:          Where component versions are stored (OCI registries, CTF archives)
    - ocm-resource-repository: Where resources within components are stored
    - input:                   How content is sourced (from files, directories, etc.) in component constructors
    - credential-repository:   Where credentials are stored and retrieved
    - signing:                 How component versions are signed and verified

HOW TO USE SUBSYSTEMS:
  When creating OCM configurations (YAML files) or using CLI commands, you'll specify a 'type'
  field. This type comes from one of the subsystems. For example:

  In a repository configuration:
    type: OCIRepository/v1        # Type from ocm-repository subsystem
    spec:
      baseUrl: ghcr.io

  In an input specification:
    type: dir/v1                  # Type from input subsystem
    spec:
      path: ./my-content

  Use this command to:
    1. Discover what subsystems exist
    2. See what types are available in each subsystem
    3. Learn what fields each type requires

EXPLORATION WORKFLOW:
  1. List all subsystems (no arguments)
  2. Pick a subsystem and list its types (one argument: subsystem name)
  3. View field details for a specific type (two arguments: subsystem and type name)

FIELD PATH NAVIGATION:
  You can drill into nested object fields using dot notation as an optional third argument.
  This shows only the fields within the specified nested structure, making it easier to explore
  complex schemas.

  Examples:
    ocm describe types ocm-repository oci baseUrl
    ocm describe types input file spec.file

OUTPUT FORMATS:
  Use -o/--output to control the format:
    - text:       Human-readable table format (default, best for terminal)
    - markdown:   Markdown tables (good for documentation)
    - html:       HTML tables (good for web publishing)
    - jsonschema: Raw JSON Schema (only for type descriptions, not lists)
    - examples:   Generate example YAML configuration (only for type descriptions)`,
		Example: `  # Workflow: Setting up an OCI repository
  # Step 1: Discover available repository types
  ocm describe types ocm-repository

  # Step 2: Learn about the OCI repository type
  ocm describe types ocm-repository oci/v1

  # Step 3: Generate example configuration
  ocm describe types ocm-repository oci/v1 -o examples

  # Workflow: Configuring input methods for component creation
  # Step 1: See what input methods are available
  ocm describe types input

  # Step 2: Learn about the directory input type
  ocm describe types input dir/v1

  # Other useful commands:
  # List all subsystems to see what's available
  ocm describe types

  # Navigate into nested configuration fields
  ocm describe types ocm-repository oci/v1 baseUrl

  # List all available field paths for navigation
  ocm describe types input file/v1 --show-paths

  # Export documentation as markdown for your team
  ocm describe types input file -o markdown > signing-docs.md`,
		RunE: run,
	}

	enum.VarP(cmd.Flags(), "output", "o", []string{"text", "markdown", "html", "jsonschema", "examples"}, "Output format (text, markdown, html are supported for all command combinations, jsonschema is only supported for type descriptions).")
	enum.Var(cmd.Flags(), "table-style", []string{table.StyleDefault.Name, table.StyleColoredDark.Name, table.StyleColoredBright.Name}, "table output style")
	cmd.Flags().Bool("show-paths", false, "List all available field paths for the type (useful for navigation)")
	return cmd
}

func run(cmd *cobra.Command, args []string) error {
	registry := ocmctx.FromContext(cmd.Context()).SubsystemRegistry()
	if registry == nil {
		return fmt.Errorf("subsystem registry not initialized")
	}

	if len(args) == 0 {
		return listSubsystems(cmd, registry)
	}
	subName := args[0]
	sub := registry.Get(subName)
	if sub == nil {
		return fmt.Errorf("unknown subsystem: %s", subName)
	}

	if len(args) == 1 {
		return listTypes(cmd, sub)
	}

	return describeType(cmd, sub, args[1:])
}

func listSubsystems(cmd *cobra.Command, registry *subsystem.Registry) error {
	w := buildSubsystemsTable(registry)
	return renderTable(cmd, w)
}

func listTypes(cmd *cobra.Command, s *subsystem.Subsystem) error {
	w := buildTypesTable(cmd, s)
	return renderTable(cmd, w)
}

func describeType(cmd *cobra.Command, s *subsystem.Subsystem, args []string) error {
	if len(args) < 1 || len(args) > 2 {
		return fmt.Errorf("expected exactly one type name and one optional field path, got: %s", args)
	}

	typ, err := runtime.TypeFromString(args[0])
	if err != nil {
		return fmt.Errorf("invalid type name %s: %w", args[0], err)
	}

	if !s.Scheme.IsRegistered(typ) {
		return fmt.Errorf("type %s is not registered in subsystem %s", typ, s.Name)
	}

	schema, compiled, err := getCompiledSchema(s, typ)
	if err != nil {
		return err
	}

	format, _ := enum.Get(cmd.Flags(), "output")

	if format == "jsonschema" {
		if _, err := cmd.OutOrStdout().Write(schema); err != nil {
			return fmt.Errorf("failed to render JSON schema: %w", err)
		}
		return nil
	}

	var path fieldpath.Path
	if len(args) == 2 {
		path, err = fieldpath.Parse(args[1])
		if err != nil {
			return fmt.Errorf("parsing field path %q failed: %w", args[1], err)
		}
	}

	current, err := navigateFieldPath(compiled, path)
	if err != nil {
		return err
	}

	info := buildSchemaInfo(typ, current, path)

	showPaths, _ := cmd.Flags().GetBool("show-paths")

	// Show field paths table
	if showPaths {
		tw := buildFieldPathsTable(current, info)
		return renderTable(cmd, tw)
	}

	// If the schema has no properties, render field details directly
	if !schemaHasProperties(current) {
		return renderFieldDetails(cmd, current, info)
	}

	tw := buildSchemaTable(current, info)
	return renderTable(cmd, tw)
}
