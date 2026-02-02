package types

import (
	"bytes"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"ocm.software/open-component-model/bindings/go/cel/expression/fieldpath"
	"ocm.software/open-component-model/bindings/go/runtime"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
	"ocm.software/open-component-model/cli/internal/flags/enum"
	"ocm.software/open-component-model/cli/internal/subsystem"
)

var styles = map[string]table.Style{
	table.StyleDefault.Name:       table.StyleDefault,
	table.StyleColoredDark.Name:   table.StyleColoredDark,
	table.StyleColoredBright.Name: table.StyleColoredBright,
}

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
  ocm describe types ocm-repository oci/v1 spec

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
	w := table.NewWriter()

	subsystems := registry.List()

	w.SetTitle("Available Subsystems")
	w.AppendHeader(table.Row{"subsystem", "types", "alias"})
	w.SetColumnConfigs([]table.ColumnConfig{
		{Number: 1, AutoMerge: true},
		{Number: 2, AutoMerge: true},
		{Number: 3},
	})
	for _, s := range subsystems {
		typs := s.Scheme.GetTypes()
		for _, typ := range slices.SortedFunc(maps.Keys(typs), runtimeSort) {
			for _, alias := range slices.SortedFunc(slices.Values(typs[typ]), runtimeSort) {
				w.AppendRow(table.Row{s.Name, typ, alias})
			}
		}
		w.AppendSeparator()
	}

	return renderTable(cmd, w)
}

func listTypes(cmd *cobra.Command, s *subsystem.Subsystem) error {
	w := table.NewWriter()
	w.SetTitle(s.Description)
	w.AppendHeader(table.Row{"type", "alias"})
	w.SetColumnConfigs([]table.ColumnConfig{
		{Number: 1, AutoMerge: true},
	})
	typs := s.Scheme.GetTypes()

	for _, typ := range slices.SortedFunc(maps.Keys(typs), runtimeSort) {
		for _, alias := range slices.SortedFunc(slices.Values(typs[typ]), runtimeSort) {
			w.AppendRow(table.Row{typ, alias})
		}
	}

	if related := subsystem.FindLinkedCommands(cmd, s.Name); len(related) > 0 {
		w.AppendFooter(table.Row{"related commands"})
		for _, related := range subsystem.FindLinkedCommands(cmd, s.Name) {
			w.AppendRow(table.Row{related})
		}
	}

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

	if format, err := enum.Get(cmd.Flags(), "output"); err == nil && format == "jsonschema" {
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

	title := buildSchemaTitle(typ, current, path)

	if showPaths, _ := cmd.Flags().GetBool("show-paths"); showPaths {
		return renderFieldPathsTable(cmd, current, title)
	}

	tw := buildSchemaTable(current, title)
	return renderTable(cmd, tw)
}

func getCompiledSchema(s *subsystem.Subsystem, typ runtime.Type) ([]byte, *jsonschema.Schema, error) {
	obj, err := s.Scheme.NewObject(typ)
	if err != nil {
		return nil, nil, err
	}

	introspectable, ok := obj.(runtime.JSONSchemaIntrospectable)
	if !ok {
		return nil, nil, fmt.Errorf("type %s does not implement JSONSchemaIntrospectable", typ)
	}

	schema := introspectable.JSONSchema()

	unmarshaled, err := jsonschema.UnmarshalJSON(bytes.NewReader(schema))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal JSON schema: %w", err)
	}

	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(typ.String(), unmarshaled); err != nil {
		return nil, nil, fmt.Errorf("failed to add resource: %w", err)
	}

	compiled, err := compiler.Compile(typ.String())
	if err != nil {
		return nil, nil, fmt.Errorf("failed to compile schema: %w", err)
	}

	return schema, compiled, nil
}

func navigateFieldPath(schema *jsonschema.Schema, path fieldpath.Path) (*jsonschema.Schema, error) {
	current := schema
	for _, segment := range path {
		if segment.Index != nil {
			return nil, fmt.Errorf("indexing not supported for schema path segments")
		}

		if candidate, ok := current.Properties[segment.Name]; ok {
			current = candidate
			continue
		}

		if current.Ref != nil {
			if candidate, ok := current.Ref.Properties[segment.Name]; ok {
				current = candidate
				continue
			}
		}

		return nil, buildPathNotFoundError(current, segment.Name)
	}
	return current, nil
}

func buildPathNotFoundError(schema *jsonschema.Schema, segmentName string) error {
	var availablePaths []string
	for propName := range schema.Properties {
		availablePaths = append(availablePaths, propName)
	}
	if schema.Ref != nil {
		for propName := range schema.Ref.Properties {
			if !slices.Contains(availablePaths, propName) {
				availablePaths = append(availablePaths, propName)
			}
		}
	}
	slices.Sort(availablePaths)

	errMsg := fmt.Sprintf("schema path segment %q not found", segmentName)
	if len(availablePaths) > 0 {
		errMsg += fmt.Sprintf("\n\nAvailable fields at this level:\n  %s", strings.Join(availablePaths, "\n  "))
	}
	return fmt.Errorf("%s", errMsg)
}

func buildSchemaTitle(typ runtime.Type, schema *jsonschema.Schema, path fieldpath.Path) string {
	var title string
	if schema.Title != "" {
		title = schema.Title + fmt.Sprintf(" (%s)", typ.String())
	} else {
		title = typ.String()
	}

	if len(path) > 0 {
		var breadcrumb strings.Builder
		breadcrumb.WriteString(typ.String())
		for _, seg := range path {
			breadcrumb.WriteString(" > ")
			breadcrumb.WriteString(seg.Name)
		}
		title = breadcrumb.String() + "\n" + title
	}

	if schema.Deprecated || (schema.Ref != nil && schema.Ref.Deprecated) {
		title += "\n⚠️  WARNING: This type is deprecated"
	}

	if schema.Description != "" {
		title += "\n" + schema.Description
	} else if schema.Ref != nil && schema.Ref.Description != "" {
		title += "\n" + schema.Ref.Description
	}

	totalFields := len(schema.Properties)
	requiredCount := len(schema.Required)
	optionalCount := totalFields - requiredCount
	if totalFields > 0 {
		title += fmt.Sprintf("\n%d fields (%d required, %d optional)", totalFields, requiredCount, optionalCount)
	}

	return title
}

func renderFieldPathsTable(cmd *cobra.Command, schema *jsonschema.Schema, title string) error {
	paths := collectFieldPaths(schema, "", 5)
	tw := table.NewWriter()
	tw.SetTitle(title + "\n\nAvailable Field Paths")
	tw.AppendHeader(table.Row{"path", "depth"})
	for _, path := range paths {
		depth := strings.Count(path, ".") + 1
		tw.AppendRow(table.Row{path, depth})
	}
	return renderTable(cmd, tw)
}

func buildSchemaTable(schema *jsonschema.Schema, title string) table.Writer {
	tw := table.NewWriter()
	tw.SetTitle(title)
	tw.AppendHeader(table.Row{"field name", "type", "required", "description"})
	tw.SetColumnConfigs([]table.ColumnConfig{
		{Number: 1, AutoMerge: true},
		{Number: 2},
		{Number: 3},
		{Number: 4, WidthMax: 100},
	})

	for _, id := range slices.Sorted(maps.Keys(schema.Properties)) {
		prop := schema.Properties[id]
		typeStr := getPropertyTypeString(prop)
		desc := getPropertyDescription(prop)
		required := "✗"
		if slices.Contains(schema.Required, id) {
			required = "✓"
		}
		tw.AppendRow(table.Row{id, typeStr, required, desc})
	}

	return tw
}

func getPropertyTypeString(prop *jsonschema.Schema) string {
	ts := ""
	if prop.Types != nil {
		ts = prop.Types.String()
	}
	if ts == "" && prop.Ref != nil && prop.Ref.Types != nil {
		ts = prop.Ref.Types.String()
	}

	if strings.Contains(ts, "object") && (len(prop.Properties) > 0 || (prop.Ref != nil && len(prop.Ref.Properties) > 0)) {
		ts += " →"
	}

	return ts
}

func getPropertyDescription(prop *jsonschema.Schema) string {
	desc := prop.Description
	if desc == "" && prop.Ref != nil {
		desc = prop.Ref.Description
	}

	propEnum := prop.Enum
	if propEnum == nil && prop.Ref != nil {
		propEnum = prop.Ref.Enum
	}
	if propEnum != nil {
		desc += "\nPossible values: " + fmt.Sprintf("%v", prop.Enum.Values)
	}

	var oneOfDesc []string
	if prop.OneOf != nil {
		for _, of := range prop.OneOf {
			if of.Const != nil {
				oneOfDesc = append(oneOfDesc, fmt.Sprintf("%v", *of.Const))
			}
		}
	}
	if prop.Ref != nil && prop.Ref.OneOf != nil {
		for _, of := range prop.Ref.OneOf {
			if of.Const != nil {
				oneOfDesc = append(oneOfDesc, fmt.Sprintf("%v", *of.Const))
			}
		}
	}
	if len(oneOfDesc) > 0 {
		desc += "\nPossible values: " + fmt.Sprintf("%v", oneOfDesc)
	}

	return desc
}

func renderTable(cmd *cobra.Command, w table.Writer) error {
	styleString, err := enum.Get(cmd.Flags(), "table-style")
	if err != nil {
		return err
	}
	style := styles[styleString]
	style.Format.Header = text.FormatUpper
	style.Format.Footer = text.FormatUpper

	out := cmd.OutOrStdout()

	if f, ok := out.(*os.File); ok {
		if width, _, err := term.GetSize(int(f.Fd())); err == nil {
			style.Size.WidthMin = width
		}
	}

	isTerminal := isTerminal(out)

	format, err := enum.Get(cmd.Flags(), "output")
	if err != nil {
		return err
	}

	w.SetStyle(style)
	var rendered string
	switch format {
	case "html":
		rendered = w.RenderHTML()
	case "markdown":
		rendered = w.RenderMarkdown()
	case "text":
		rendered = w.Render()
	default:
		return fmt.Errorf("unknown output format: %s", format)
	}

	if !isTerminal {
		_, err := io.WriteString(out, rendered)
		return err
	}
	return renderWithPager(cmd, rendered)
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

var pager = sync.OnceValue(func() string {
	return os.Getenv("PAGER")
})

func renderWithPager(cmd *cobra.Command, rendered string) error {
	out := cmd.OutOrStdout()

	if !isTerminal(out) {
		_, err := io.WriteString(out, rendered)
		return err
	}

	pager := pager()
	if pager == "" {
		pager = "less"
	}

	args := strings.Fields(pager)
	cmdName := args[0]

	if cmdName == "less" {
		args = append(args, "-R", "-F", "-X")
	}

	pagerCmd := exec.CommandContext(cmd.Context(), cmdName, args[1:]...)
	pagerCmd.Stdin = strings.NewReader(rendered)
	pagerCmd.Stdout = out
	pagerCmd.Stderr = os.Stderr

	if err := pagerCmd.Start(); err != nil {
		_, werr := io.WriteString(out, rendered)
		return werr
	}

	return pagerCmd.Wait()
}

// collectFieldPaths recursively collects all field paths from a schema
func collectFieldPaths(schema *jsonschema.Schema, prefix string, maxDepth int) []string {
	if schema == nil || maxDepth <= 0 {
		return nil
	}

	var paths []string
	properties := schema.Properties
	if len(properties) == 0 && schema.Ref != nil {
		properties = schema.Ref.Properties
	}

	for propName, prop := range properties {
		currentPath := propName
		if prefix != "" {
			currentPath = prefix + "." + propName
		}
		paths = append(paths, currentPath)

		// Recursively collect nested paths for object types (check if has properties)
		if len(prop.Properties) > 0 {
			paths = append(paths, collectFieldPaths(prop, currentPath, maxDepth-1)...)
		} else if prop.Ref != nil && len(prop.Ref.Properties) > 0 {
			paths = append(paths, collectFieldPaths(prop.Ref, currentPath, maxDepth-1)...)
		}
	}

	slices.Sort(paths)
	return paths
}

var runtimeSort = func(a, b runtime.Type) int {
	return strings.Compare(a.String(), b.String())
}
