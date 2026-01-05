package types

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"ocm.software/open-component-model/cli/internal/render/jsonschema"
	"ocm.software/open-component-model/cli/internal/subsystem"
)

// MarkdownRenderer renders documentation as Markdown.
type MarkdownRenderer struct {
	rootCommand *cobra.Command
}

func (r *MarkdownRenderer) SetRootCommand(cmd *cobra.Command) {
	r.rootCommand = cmd
}

func (r *MarkdownRenderer) RenderSubsystem(w io.Writer, s *subsystem.Subsystem) error {
	fmt.Fprintf(w, "# Subsystem: %s\n\n", s.Title)
	fmt.Fprintf(w, "**ID**: `%s`\n\n", s.Name)
	if s.Description != "" {
		fmt.Fprintf(w, "%s\n\n", s.Description)
	}

	if len(s.Guides) > 0 {
		fmt.Fprintln(w, "## Usage Guides")
		for _, g := range s.Guides {
			fmt.Fprintf(w, "### %s\n\n", g.Title)
			if g.Summary != "" {
				fmt.Fprintf(w, "%s\n\n", g.Summary)
			}
			for _, sec := range g.Sections {
				fmt.Fprintf(w, "#### %s\n\n", sec.Title)
				fmt.Fprintf(w, "%s\n\n", sec.Content)
				if sec.Example != nil {
					fmt.Fprintf(w, "**Example (%s):**\n", sec.Example.Caption)
					fmt.Fprintf(w, "```%s\n%s\n```\n\n", sec.Example.Language, sec.Example.Content)
				}
			}
		}
	}

	if r.rootCommand != nil {
		linkedCmds := subsystem.FindLinkedCommands(r.rootCommand, s.Name)
		if len(linkedCmds) > 0 {
			fmt.Fprintln(w, "## Relevant CLI Commands")
			for _, c := range linkedCmds {
				fmt.Fprintf(w, "- `%s`: %s\n", c.CommandPath(), c.Short)
			}
			fmt.Fprintln(w)
		}
	}

	return nil
}

func (r *MarkdownRenderer) RenderType(w io.Writer, s *subsystem.Subsystem, name string, doc *jsonschema.TypeDoc) error {
	fmt.Fprintf(w, "# Type: `%s`\n\n", name)
	fmt.Fprintf(w, "**Subsystem**: [%s](subsystems/%s.md) | **Title**: `%s`\n\n", s.Title, s.Name, doc.Title)

	if doc.Description != "" {
		fmt.Fprintf(w, "%s\n\n", doc.Description)
	}

	if len(doc.Properties) > 0 {
		fmt.Fprintln(w, "## Fields")
		fmt.Fprintln(w, "| Field Name | Type | Required | Description |")
		fmt.Fprintln(w, "| :--- | :--- | :--- | :--- |")
		for _, p := range doc.Properties {
			req := "No"
			if p.Required {
				req = "**Yes**"
			}
			typStr := p.Type
			if p.Type == "array" && p.ItemsType != "" {
				typStr = fmt.Sprintf("[]%s", p.ItemsType)
			}

			desc := p.Description
			if len(p.Enum) > 0 {
				desc += fmt.Sprintf(" (Enum: `%v`)", p.Enum)
			}
			if p.Default != nil {
				desc += fmt.Sprintf(" (Default: `%v`)", p.Default)
			}

			fmt.Fprintf(w, "| `%s` | %s | %s | %s |\n", p.Path, typStr, req, strings.ReplaceAll(desc, "\n", " "))
		}
		fmt.Fprintln(w)
	}

	return nil
}
