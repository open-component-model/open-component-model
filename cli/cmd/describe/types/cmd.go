package types

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	"ocm.software/open-component-model/bindings/go/cel/expression/fieldpath"
	"ocm.software/open-component-model/cli/internal/flags/enum"

	"ocm.software/open-component-model/bindings/go/runtime"
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
		Use:     "types [subsystem [type]]",
		Aliases: []string{"type"},
		Short:   "Describe OCM types and their configuration schema",
		Long: `Describe OCM types registered in various subsystems.
If no subsystem is specified, it lists all available subsystems.
If a subsystem is specified, it lists all types in that subsystem.
If both subsystem and type are specified, it shows detailed documentation for that type.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(cmd, args)
		},
	}

	enum.VarP(cmd.Flags(), "output", "o", []string{"text", "markdown", "html", "jsonschema"}, "Output format (text, markdown, html).")
	enum.Var(cmd.Flags(), "table-style", []string{table.StyleColoredDark.Name, table.StyleColoredBright.Name, table.StyleDefault.Name}, "table output style")
	return cmd
}

func run(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return listSubsystems(cmd)
	}
	subName := args[0]
	sub := subsystem.Get(subName)
	if sub == nil {
		return fmt.Errorf("unknown subsystem: %s", subName)
	}

	if len(args) == 1 {
		return listTypes(cmd, sub)
	}

	return describeType(cmd, sub, args[1:])
}

func listSubsystems(cmd *cobra.Command) error {
	w := table.NewWriter()

	subsystems := subsystem.List()

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
	typeName := args[0]

	typ, err := runtime.TypeFromString(typeName)
	if err != nil {
		return fmt.Errorf("invalid type name %s: %w", typeName, err)
	}

	if !s.Scheme.IsRegistered(typ) {
		return fmt.Errorf("type %s is not registered in subsystem %s", typ, s.Name)
	}

	obj, err := s.Scheme.NewObject(typ)
	if err != nil {
		return err
	}

	introspectable, ok := obj.(runtime.JSONSchemaIntrospectable)
	if !ok {
		return fmt.Errorf("type %s does not implement JSONSchemaIntrospectable", typ)
	}

	schema := introspectable.JSONSchema()

	unmarshaled, err := jsonschema.UnmarshalJSON(bytes.NewReader(schema))
	if err != nil {
		return fmt.Errorf("failed to unmarshal JSON schema: %w", err)
	}

	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(typ.String(), unmarshaled); err != nil {
		return fmt.Errorf("failed to add resource: %w", err)
	}

	compiled, err := compiler.Compile(typ.String())
	if err != nil {
		return fmt.Errorf("failed to compile schema: %w", err)
	}

	if format, err := enum.Get(cmd.Flags(), "output"); err == nil && format == "jsonschema" {
		_, err := cmd.OutOrStdout().Write(schema)
		return fmt.Errorf("failed to render JSON schema: %w", err)
	}

	current := compiled

	var path fieldpath.Path
	if len(args) == 2 {
		var err error
		path, err = fieldpath.Parse(args[1])
		if err != nil {
			return fmt.Errorf("parsing field path %q failed: %w", args[1], err)
		}
	}
	if len(path) > 0 {
		for _, segment := range path {
			if segment.Index != nil {
				return fmt.Errorf("indexing not supported for schema path segments")
			}
			if current, ok = current.Properties[segment.Name]; ok {
				continue
			}
			if current, ok = current.Ref.Properties[segment.Name]; ok {
				continue
			}
			return fmt.Errorf("schema path segment %q not found (based on %q)", segment.Name, path)
		}
	}

	var title string
	if current.Title != "" {
		title = current.Title + fmt.Sprintf(" (%s)", typ.String())
	} else {
		title = typ.String()
	}
	if current.Description != "" {
		title += "\n" + current.Description
	}

	tw := table.NewWriter()
	tw.SetTitle(title)

	tw.AppendHeader(table.Row{"field name", "type", "required", "description"})
	tw.SetColumnConfigs([]table.ColumnConfig{
		{Number: 1, AutoMerge: true},
		{Number: 2},
		{Number: 3},
		{Number: 4, WidthMax: 100},
	})
	for id, prop := range current.Properties {
		ts := ""
		if typ := prop.Types; typ != nil {
			ts = typ.String()
		}
		if ts == "" && prop.Ref != nil && prop.Ref.Types != nil {
			ts = prop.Ref.Types.String()
		}

		desc := prop.Description
		if desc == "" && prop.Ref != nil {
			desc = prop.Ref.Description
		}

		enum := prop.Enum
		if enum == nil && prop.Ref != nil {
			enum = prop.Ref.Enum
		}
		if enum != nil {
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

		tw.AppendRow(table.Row{id, ts, slices.Contains(current.Required, id), desc})
	}

	return renderTable(cmd, tw)
}

func renderTable(cmd *cobra.Command, w table.Writer) error {
	styleString, err := enum.Get(cmd.Flags(), "table-style")
	if err != nil {
		return err
	}
	style := styles[styleString]
	style.Format.Header = text.FormatUpper
	style.Format.Footer = text.FormatUpper

	w.SetStyle(style)
	if adjustStyleToCMD(cmd, &style) != nil {
		return fmt.Errorf("failed to set max width for table")
	}
	w.SetStyle(style)

	format, err := enum.Get(cmd.Flags(), "output")
	if err != nil {
		return err
	}

	var out string
	switch format {
	case "html":
		out = w.RenderHTML()
	case "markdown":
		out = w.RenderMarkdown()
	case "text":
		out = w.Render()
	default:
		return fmt.Errorf("unknown output format: %s", format)
	}
	_, err = cmd.OutOrStdout().Write([]byte(out))
	return err
}

func adjustStyleToCMD(cmd *cobra.Command, style *table.Style) error {
	if f, ok := cmd.OutOrStdout().(*os.File); ok {
		width, _, err := term.GetSize(int(f.Fd()))
		if err != nil {
			return fmt.Errorf("failed to get terminal size: %w", err)
		}
		style.Size.WidthMax = width
		style.Size.WidthMin = width
	}
	return nil
}

var runtimeSort = func(a, b runtime.Type) int {
	return strings.Compare(a.String(), b.String())
}
