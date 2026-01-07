package types

import (
	"bytes"
	"fmt"
	"slices"
	"sort"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/spf13/cobra"
	"ocm.software/open-component-model/bindings/go/cel/expression/fieldpath"

	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/internal/subsystem"
)

// New represents the command to describe OCM types.
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "types [subsystem [type]]",
		Aliases: []string{"get"},
		Short:   "Describe OCM types and their configuration schema",
		Long: `Describe OCM types registered in various subsystems.
If no subsystem is specified, it lists all available subsystems.
If a subsystem is specified, it lists all types in that subsystem.
If both subsystem and type are specified, it shows detailed documentation for that type.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(cmd, args)
		},
	}

	cmd.Flags().StringP("output", "o", "text", "Output format (text, markdown, html).")
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

const colorFmt = "\033[1;36m%s\033[0m"

func listSubsystems(cmd *cobra.Command) error {
	w := table.NewWriter()
	w.SetOutputMirror(cmd.OutOrStdout())
	style := table.StyleDefault
	style.Box.PaddingLeft = ""
	style.Options.DrawBorder = false
	style.Options.SeparateRows = false
	style.Options.SeparateColumns = true
	style.Options.SeparateHeader = true

	subsystems := subsystem.List()
	sort.Slice(subsystems, func(i, j int) bool { return subsystems[i].Name < subsystems[j].Name })

	w.AppendHeader(table.Row{fmt.Sprintf(colorFmt, "SUBSYSTEM"), fmt.Sprintf(colorFmt, "TYPES")})
	w.SetColumnConfigs([]table.ColumnConfig{
		{Number: 1, AutoMerge: true},
	})
	for _, s := range subsystems {
		rows := make([]table.Row, 0, len(s.Scheme.GetTypes()))
		for typ, aliases := range s.Scheme.GetTypes() {
			rows = append(rows, table.Row{s.Name, fmt.Sprintf("%s (%d aliases)", typ, len(aliases))})
		}
		w.AppendRows(rows)
		w.AppendSeparator()
	}

	return renderTable(cmd, w)
}

func listTypes(cmd *cobra.Command, s *subsystem.Subsystem) error {

	w := table.NewWriter()
	w.SetOutputMirror(cmd.OutOrStdout())
	w.Style().Title.Align = text.AlignCenter
	w.SetTitle(fmt.Sprintf(colorFmt, s.Name) + "\n" + s.Title)
	w.AppendHeader(table.Row{fmt.Sprintf(colorFmt, "TYPE"), fmt.Sprintf(colorFmt, "ALIAS")})
	w.SetColumnConfigs([]table.ColumnConfig{
		{Number: 1, AutoMerge: true},
	})
	for typ, aliases := range s.Scheme.GetTypes() {
		for _, alias := range aliases {
			w.AppendRow(table.Row{typ, alias})
		}
	}

	if related := subsystem.FindLinkedCommands(cmd, s.Name); len(related) > 0 {
		w.AppendFooter(table.Row{"RELATED COMMANDS"})
		for _, related := range subsystem.FindLinkedCommands(cmd, s.Name) {
			w.AppendRow(table.Row{related})
		}
	}

	return renderTable(cmd, w)
}

func renderTable(cmd *cobra.Command, w table.Writer) error {
	switch outFormat, _ := cmd.Flags().GetString("output"); outFormat {
	case "html":
		w.RenderHTML()
	case "markdown":
		w.RenderMarkdown()
	case "text":
		w.Render()
	default:
		return fmt.Errorf("unknown output format: %s", outFormat)
	}
	return nil
}

func describeType(cmd *cobra.Command, s *subsystem.Subsystem, args []string) error {
	if len(args) < 1 || len(args) > 2 {
		return fmt.Errorf("expected exactly one type name and one optional field path, got: %s", args)
	}
	typeName := args[0]

	var path fieldpath.Path
	if len(args) == 2 {
		var err error
		path, err = fieldpath.Parse(args[1])
		if err != nil {
			return fmt.Errorf("parsing field path %q failed: %w", args[1], err)
		}
	}

	typ, err := runtime.TypeFromString(typeName)
	if err != nil {
		return fmt.Errorf("invalid type name %s: %w", typeName, err)
	}

	obj, err := s.Scheme.NewObject(typ)
	if err != nil {
		return fmt.Errorf("failed to create object for type %s: %w", typ, err)
	}

	introspectable, ok := obj.(runtime.JSONSchemaIntrospectable)
	if !ok {
		return fmt.Errorf("type %s does not implement JSONSchemaIntrospectable", typ)
	}

	schema := introspectable.JSONSchema()

	tw := table.NewWriter()
	tw.SetOutputMirror(cmd.OutOrStdout())

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

	current := compiled

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
	title = fmt.Sprintf(colorFmt, title)
	if current.Description != "" {
		title += "\n" + current.Description
	}

	tw.SetTitle(title)

	tw.AppendHeader(table.Row{"Field Name", "Type", "Required", "Description"})
	for id, prop := range current.Properties {
		ts := ""
		if typ := prop.Types; typ != nil {
			ts = typ.String()
		}

		desc := prop.Description

		if prop.Enum != nil {
			desc += "\nPossible values: " + fmt.Sprintf("%v", prop.Enum.Values)
		}
		if prop.OneOf != nil {
			var oneOfDesc []string
			for _, of := range prop.OneOf {
				if of.Const != nil {
					oneOfDesc = append(oneOfDesc, fmt.Sprintf("%v", *of.Const))
				}
			}
			desc += "\nPossible values: " + fmt.Sprintf("%v", oneOfDesc)
		}

		tw.AppendRow(table.Row{id, ts, slices.Contains(current.Required, id), desc})
	}

	return renderTable(cmd, tw)
}
