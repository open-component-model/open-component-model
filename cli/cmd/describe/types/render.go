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

	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/internal/flags/enum"
	"ocm.software/open-component-model/cli/internal/subsystem"
)

var styles = map[string]table.Style{
	table.StyleDefault.Name:       table.StyleDefault,
	table.StyleColoredDark.Name:   table.StyleColoredDark,
	table.StyleColoredBright.Name: table.StyleColoredBright,
}

var runtimeSort = func(a, b runtime.Type) int {
	return strings.Compare(a.String(), b.String())
}

// getCompiledSchema retrieves and compiles the JSON schema for a type.
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

// buildSubsystemsTable builds a table of all subsystems.
func buildSubsystemsTable(registry *subsystem.Registry) table.Writer {
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

	return w
}

// buildTypesTable builds a table of types for a subsystem.
func buildTypesTable(cmd *cobra.Command, s *subsystem.Subsystem) table.Writer {
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

	return w
}

// buildSchemaTable builds a table displaying schema properties.
func buildSchemaTable(schema *jsonschema.Schema, info schemaInfo) table.Writer {
	tw := table.NewWriter()
	tw.SetTitle(info.formatTableTitle())
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
		required := "no"
		if slices.Contains(schema.Required, id) {
			required = "yes"
		}
		tw.AppendRow(table.Row{id, typeStr, required, desc})
	}

	return tw
}

// buildFieldPathsTable builds a table displaying available field paths.
func buildFieldPathsTable(schema *jsonschema.Schema, info schemaInfo) table.Writer {
	paths := collectFieldPaths(schema, "", 5)
	tw := table.NewWriter()
	tw.SetTitle(info.formatTableTitle() + "\n\nAvailable Field Paths")
	tw.AppendHeader(table.Row{"path", "depth"})
	for _, path := range paths {
		depth := strings.Count(path, ".") + 1
		tw.AppendRow(table.Row{path, depth})
	}
	return tw
}

// formatFieldDetails formats a single field's details as text.
func formatFieldDetails(schema *jsonschema.Schema, info schemaInfo) string {
	var sb strings.Builder

	// Breadcrumb path (if navigated into a field)
	if info.Breadcrumb != "" {
		sb.WriteString(info.Breadcrumb)
		sb.WriteString("\n\n")
	}

	// Title
	if info.Title != "" && info.Title != info.Type {
		sb.WriteString(info.Title)
		sb.WriteString("\n")
	}

	// Type
	typeStr := getPropertyTypeString(schema)
	if typeStr != "" {
		sb.WriteString("Type: ")
		sb.WriteString(typeStr)
		sb.WriteString("\n")
	}

	// Deprecation warning
	if info.Deprecated {
		sb.WriteString("\nWARNING: This field is deprecated\n")
	}

	// Description
	if info.Description != "" {
		sb.WriteString("\n")
		sb.WriteString(info.Description)
		sb.WriteString("\n")
	}

	// Additional constraints (enums, oneOf)
	constraints := getFieldConstraints(schema)
	if constraints != "" {
		sb.WriteString("\n")
		sb.WriteString(constraints)
		sb.WriteString("\n")
	}

	return sb.String()
}

// renderTable renders a table to the command output with appropriate styling.
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

	if isTerminal(out) {
		return renderWithPager(cmd, rendered)
	}

	_, err = io.WriteString(out, rendered)
	return err
}

// renderFieldDetails renders a single field's details to the command output.
func renderFieldDetails(cmd *cobra.Command, schema *jsonschema.Schema, info schemaInfo) error {
	out := cmd.OutOrStdout()
	content := formatFieldDetails(schema, info)
	if isTerminal(out) {
		return renderWithPager(cmd, content)
	}
	_, err := io.WriteString(out, content)
	return err
}

// isTerminal checks if the writer is connected to a terminal.
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

// renderWithPager renders content through a pager if available.
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
