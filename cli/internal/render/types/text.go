package types

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"text/template"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/spf13/cobra"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/internal/subsystem"
)

const (
	colorBold  = "\033[1m"
	colorReset = "\033[0m"
)

var (
	subsystemTemplate = `{{bold "TITLE :"}} {{bold .Subsystem.Title}}
{{bold "SUBSYSTEM:"}} {{.Subsystem.Name}}

{{if .Subsystem.Description}}{{bold "SUMMARY:"}}
{{indent .Subsystem.Description 2}}

{{end}}{{if .LinkedCommands}}{{bold "RELEVANT CLI COMMANDS"}}

{{range .LinkedCommands}}  - {{bold .CommandPath}}
{{if .Short}}    {{.Short}}
{{end}}{{end}}
{{end}}`
)

// TextRenderer renders documentation as ANSI-colored text for the terminal.
type TextRenderer struct {
	rootCommand *cobra.Command
	subTemplate *template.Template
}

func NewTextRenderer() *TextRenderer {
	funcMap := template.FuncMap{
		"bold": func(s string) string {
			return colorBold + s + colorReset
		},
		"indent": func(s string, n int) string {
			res := ""
			lines := strings.Split(s, "\n")
			prefix := strings.Repeat(" ", n)
			for i, line := range lines {
				if i > 0 {
					res += "\n"
				}
				res += prefix + line
			}
			return res
		},
	}

	return &TextRenderer{
		subTemplate: template.Must(template.New("subsystem").Funcs(funcMap).Parse(subsystemTemplate)),
	}
}

func (r *TextRenderer) SetRootCommand(cmd *cobra.Command) {
	r.rootCommand = cmd
}

func (r *TextRenderer) RenderSubsystem(w io.Writer, s *subsystem.Subsystem) error {
	data := struct {
		Subsystem      *subsystem.Subsystem
		LinkedCommands []*cobra.Command
	}{
		Subsystem: s,
	}

	if r.rootCommand != nil {
		data.LinkedCommands = subsystem.FindLinkedCommands(r.rootCommand, s.Name)
	}

	return r.subTemplate.Execute(w, data)
}

func (r *TextRenderer) RenderType(w io.Writer, s *subsystem.Subsystem, typ runtime.Type, schema io.Reader) error {
	tw := table.NewWriter()
	tw.SetOutputMirror(w)

	unmarshaled, err := jsonschema.UnmarshalJSON(schema)
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
	// TODO recursive descent based on optional field path

	var title string
	if current.Title != "" {
		title = colorBold + current.Title + colorReset + fmt.Sprintf(" (%s)", typ.String())
	} else {
		title = colorBold + typ.String() + colorReset
	}
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

		tw.AppendRow(table.Row{colorBold + id + colorReset, ts, slices.Contains(current.Required, id), desc})
	}

	tw.Render()
	return nil
}
