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
	"ocm.software/open-component-model/cli/internal/flags/enum"
	"sigs.k8s.io/yaml"

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

	enum.VarP(cmd.Flags(), "output", "o", []string{"text", "markdown", "html", "jsonschema", "examples"}, "Output format (text, markdown, html are supported for all command combinations, jsonschema is only supported for type descriptions).")
	enum.Var(cmd.Flags(), "table-style", []string{table.StyleColoredDark.Name, table.StyleColoredBright.Name, table.StyleDefault.Name}, "table output style")
	cmd.Flags().Bool("example", false, "shows an example for the given type and schema. if multiple examples are defined, all are shown")
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
		if _, err := cmd.OutOrStdout().Write(schema); err != nil {
			return fmt.Errorf("failed to render JSON schema: %w", err)
		}
		return nil
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

			return fmt.Errorf("schema path segment %q not found (based on %s)", segment.Name, path)
		}
	}

	if showExamples, _ := cmd.Flags().GetBool("example"); showExamples {
		if len(current.Examples) == 0 {
			return fmt.Errorf("no examples available for type %s", typ)
		}
		var data []byte
		for i, example := range current.Examples {
			if i > 0 {
				if _, err = cmd.OutOrStdout().Write([]byte("---\n")); err != nil {
					return fmt.Errorf("failed to write separator: %w", err)
				}
			}
			data, err = yaml.Marshal(example)
			if err != nil {
				return fmt.Errorf("failed to marshal example: %w", err)
			}
			if _, err = cmd.OutOrStdout().Write(data); err != nil {
				return fmt.Errorf("failed to write example: %w", err)
			}
		}
		return nil
	}

	var title string
	if current.Title != "" {
		title = current.Title + fmt.Sprintf(" (%s)", typ.String())
	} else {
		title = typ.String()
	}
	if current.Description != "" {
		title += "\n" + current.Description
	} else if current.Ref != nil && current.Ref.Description != "" {
		title += "\n" + current.Ref.Description
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
	for _, id := range slices.Sorted(maps.Keys(current.Properties)) {
		prop := current.Properties[id]
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

var runtimeSort = func(a, b runtime.Type) int {
	return strings.Compare(a.String(), b.String())
}
