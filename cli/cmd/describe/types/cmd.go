package types

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/jedib0t/go-pretty/v6/list"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
	render "ocm.software/open-component-model/cli/internal/render/types"

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

	cmd.Flags().StringP("output", "o", "text", "Output format (text, jsonschema).")
	return cmd
}

func getRenderer(cmd *cobra.Command) (render.DocRenderer, error) {
	outFormat, _ := cmd.Flags().GetString("output")
	switch outFormat {
	case "text":
		return render.NewTextRenderer(), nil
	case "jsonschema":
		return render.NewSchemaRenderer(), nil
	default:
		return nil, fmt.Errorf("unknown output format: %s", outFormat)
	}
}

func run(cmd *cobra.Command, args []string) error {
	renderer, err := getRenderer(cmd)
	if err != nil {
		return err
	}
	renderer.SetRootCommand(cmd)

	if len(args) == 0 {
		return listSubsystems(cmd)
	}
	subName := args[0]
	sub := subsystem.Get(subName)
	if sub == nil {
		return fmt.Errorf("unknown subsystem: %s", subName)
	}

	if len(args) == 1 {
		return listTypes(cmd, sub, renderer)
	}

	typeName := args[1]
	return describeType(cmd, sub, typeName, renderer)
}

func listSubsystems(cmd *cobra.Command) error {
	w := list.NewWriter()
	w.SetOutputMirror(cmd.OutOrStdout())
	w.SetStyle(list.StyleDefault)

	subsystems := subsystem.List()
	sort.Slice(subsystems, func(i, j int) bool { return subsystems[i].Name < subsystems[j].Name })

	for _, s := range subsystems {
		w.AppendItem(fmt.Sprintf("\033[1m%-25s\033[0m %s", s.Name, s.Title))
		w.Indent()
		for typ, aliases := range s.Scheme.GetTypes() {
			w.AppendItem(fmt.Sprintf("%s (%d aliases)", typ, len(aliases)))
		}
		w.UnIndent()
	}
	w.Render()
	return nil
}

func listTypes(cmd *cobra.Command, s *subsystem.Subsystem, renderer render.DocRenderer) error {
	if err := renderer.RenderSubsystem(cmd.OutOrStdout(), s); err != nil {
		return err
	}

	switch outFormat, _ := cmd.Flags().GetString("output"); outFormat {
	case "text":
		colorFmt := "\033[1;36m%s\033[0m"
		w := table.NewWriter()
		w.SetOutputMirror(cmd.OutOrStdout())
		style := table.StyleDefault
		style.Box.PaddingLeft = ""
		style.Options.DrawBorder = false
		style.Options.SeparateRows = false
		style.Options.SeparateColumns = true
		style.Options.SeparateHeader = true
		w.SetStyle(style)
		w.AppendHeader(table.Row{fmt.Sprintf(colorFmt, "TYPE"), fmt.Sprintf(colorFmt, "ALIASES")})
		for typ, aliases := range s.Scheme.GetTypes() {
			w.AppendRow(table.Row{typ, join(aliases)})
		}
		w.Render()
	default:
		return fmt.Errorf("unknown output format: %s", outFormat)
	}
	return nil
}

func join[T fmt.Stringer](base []T) string {
	conv := make([]string, len(base))
	for i, item := range base {
		conv[i] = item.String()
	}
	return strings.Join(conv, ",")
}

func describeType(cmd *cobra.Command, s *subsystem.Subsystem, typeName string, renderer render.DocRenderer) error {
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

	return renderer.RenderType(cmd.OutOrStdout(), s, typ, bytes.NewReader(schema))
}
