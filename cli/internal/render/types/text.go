package types

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"text/template"

	"github.com/spf13/cobra"
	"ocm.software/open-component-model/cli/internal/render/jsonschema"
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

{{end}}{{if .Subsystem.Guides}}{{bold "USAGE GUIDES & DOCUMENTATION"}}
{{range .Subsystem.Guides}}
{{bold "GUIDE:"}} {{.Title}}
{{if .Summary}}  {{bold .Summary}}
{{end}}{{range .Sections}}
  {{bold .Title}}
{{if .Content}}{{indent .Content 4}}

{{end}}{{if .Example}}    {{bold "Example: "}}{{bold .Example.Caption}}
{{indent .Example.Content 6}}
{{end}}{{end}}{{end}}
{{end}}{{if .LinkedCommands}}{{bold "RELEVANT CLI COMMANDS"}}

{{range .LinkedCommands}}  - {{bold .CommandPath}}
{{if .Short}}    {{.Short}}
{{end}}{{end}}
{{end}}`

	typeTemplate = `{{bold "TYPE:   "}} {{.Name}}
{{bold "SYSTEM: "}} {{.Subsystem.Name}} ({{.Subsystem.Title}})

{{if .Doc.Description}}{{bold "DESCRIPTION:"}}
{{indent .Doc.Description 2}}

{{end}}{{if .Properties}}{{bold "SCHEMA FIELDS"}}

  {{bold "Field Name                Type       Req   Description"}}
  {{bold "----------                ----       ---   -----------"}}
{{range .Properties}}  {{printf "%-25s %-10s %-5s %s" .Path .DisplayType .ReqStr .FirstDesc}}
{{if .RemainingDesc}}{{indent .RemainingDesc 29}}
{{end}}{{if .Default}}                             {{bold "Default: "}}{{.Default}}
{{end}}{{if .Enum}}                             {{bold "Enum:    "}}{{.Enum}}
{{end}}{{end}}
{{end}}`
)

// TextRenderer renders documentation as ANSI-colored text for the terminal.
type TextRenderer struct {
	rootCommand *cobra.Command
	subTemplate *template.Template
	typTemplate *template.Template
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
		typTemplate: template.Must(template.New("type").Funcs(funcMap).Parse(typeTemplate)),
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

type propData struct {
	Path          string
	DisplayType   string
	ReqStr        string
	FirstDesc     string
	RemainingDesc string
	Default       interface{}
	Enum          []interface{}
}

func (r *TextRenderer) RenderType(w io.Writer, s *subsystem.Subsystem, name string, doc *jsonschema.TypeDoc) error {
	var props []propData
	if len(doc.Properties) > 0 {
		sortedProps := append([]jsonschema.PropertyDoc{}, doc.Properties...)
		sort.Slice(sortedProps, func(i, j int) bool { return sortedProps[i].Path < sortedProps[j].Path })

		for _, p := range sortedProps {
			reqStr := "[ ]"
			if p.Required {
				reqStr = "[x]"
			}
			typ := p.Type
			if p.Type == "array" && p.ItemsType != "" {
				typ = fmt.Sprintf("[]%s", p.ItemsType)
			}

			descLines := strings.Split(p.Description, "\n")
			first := ""
			remaining := ""
			if len(descLines) > 0 {
				first = descLines[0]
				if len(descLines) > 1 {
					remaining = strings.Join(descLines[1:], "\n")
				}
			}

			props = append(props, propData{
				Path:          p.Path,
				DisplayType:   typ,
				ReqStr:        reqStr,
				FirstDesc:     first,
				RemainingDesc: remaining,
				Default:       p.Default,
				Enum:          p.Enum,
			})
		}
	}

	data := struct {
		Name       string
		Subsystem  *subsystem.Subsystem
		Doc        *jsonschema.TypeDoc
		Properties []propData
	}{
		Name:       name,
		Subsystem:  s,
		Doc:        doc,
		Properties: props,
	}

	return r.typTemplate.Execute(w, data)
}
