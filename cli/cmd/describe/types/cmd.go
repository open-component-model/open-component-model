package types

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	render "ocm.software/open-component-model/cli/internal/render/types"

	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/internal/render/jsonschema"
	"ocm.software/open-component-model/cli/internal/subsystem"
)

// New represents the command to describe OCM types.
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "types [subsystem] [type]",
		Short: "Describe OCM types and their configuration schema",
		Long: `Describe OCM types registered in various subsystems.
If no subsystem is specified, it lists all available subsystems.
If a subsystem is specified, it lists all types in that subsystem.
If both subsystem and type are specified, it shows detailed documentation for that type.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(cmd, args)
		},
	}
	cmd.Flags().StringP("output", "o", "text", "Output format (text, markdown)")
	return cmd
}

func getRenderer(cmd *cobra.Command) (render.DocRenderer, error) {
	outFormat, _ := cmd.Flags().GetString("output")
	switch outFormat {
	case "text":
		return render.NewTextRenderer(), nil
	case "markdown":
		return &render.MarkdownRenderer{}, nil
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
	fmt.Fprintln(cmd.OutOrStdout(), "Available subsystems:")
	list := subsystem.List()
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })

	for _, s := range list {
		fmt.Fprintf(cmd.OutOrStdout(), "  - \033[1m%-25s\033[0m %s\n", s.Name, s.Title)
	}
	return nil
}

func listTypes(cmd *cobra.Command, s *subsystem.Subsystem, renderer render.DocRenderer) error {
	if err := renderer.RenderSubsystem(cmd.OutOrStdout(), s); err != nil {
		return err
	}

	outFormat, _ := cmd.Flags().GetString("output")
	if outFormat == "markdown" {
		fmt.Fprintf(cmd.OutOrStdout(), "## Available Types\n\n")
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "\033[1;36mAVAILABLE TYPES:\033[0m\n")
	}

	types := s.Scheme.GetTypes()
	var names []string
	for t := range types {
		names = append(names, t.String())
	}
	sort.Strings(names)

	for _, n := range names {
		if outFormat == "markdown" {
			fmt.Fprintf(cmd.OutOrStdout(), "- [%s](#type-%s)\n", n, strings.ReplaceAll(strings.ToLower(n), "/", ""))
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "  - %s\n", n)
		}
	}
	return nil
}

func describeType(cmd *cobra.Command, s *subsystem.Subsystem, typeName string, renderer render.DocRenderer) error {
	typ, err := runtime.TypeFromString(typeName)
	if err != nil {
		return fmt.Errorf("invalid type name %s: %w", typeName, err)
	}

	doc, err := jsonschema.FromType(s.Scheme, typ)
	if err != nil {
		return fmt.Errorf("failed to extract documentation for type %s: %w", typeName, err)
	}
	if doc == nil {
		return fmt.Errorf("type %s does not provide documentation (no JSON Schema)", typeName)
	}

	return renderer.RenderType(cmd.OutOrStdout(), s, typeName, doc)
}
