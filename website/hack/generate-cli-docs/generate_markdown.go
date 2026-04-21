package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"ocm.software/ocm/api/utils/cobrautils"
)

// Defines the shared frontmatter template for generated CLI docs.
const fmTmpl = `---
title: %s
name: %s
url: %s
draft: false
weight: 55
toc: true
sidebar:
  collapsed: true
---
`

// Walks the CLI command tree and writes markdown pages for each command.
func genMarkdownTreeCustom(cmd *cobra.Command, dir, parentCmd string, cmdToLink map[string]string) (err error) {
	for _, c := range cmd.Commands() {
		if !c.IsAvailableCommand() && !c.IsAdditionalHelpTopicCommand() {
			continue
		}

		parent := commandToID(c.Parent().CommandPath())
		if err := genMarkdownTreeCustom(c, dir, parent, cmdToLink); err != nil {
			return err
		}
	}

	var basename string

	if cmd.IsAdditionalHelpTopicCommand() {
		basename = commandToID(cmd.CommandPath()) + ".md"
		dir = path.Join(dir, "help")
	} else if cmd.HasAvailableSubCommands() && !cmd.HasParent() {
		basename = "_index.md"
	} else if cmd.HasAvailableSubCommands() && cmd.HasParent() {
		basename = "_index.md"
		dir = path.Join(dir, commandToDir(cmd.CommandPath()))
	} else if cmd.HasParent() && !cmd.IsAdditionalHelpTopicCommand() {
		dir = path.Join(dir, commandToDir(cmd.Parent().CommandPath()))
		basename = commandToID(cmd.CommandPath()) + ".md"
	} else if cmd.HasParent() && cmd.IsAdditionalHelpTopicCommand() {
		// Put the additional topic into where the command is sitting and not where
		// subcommand would be.
		// ocm/add instead of ocm/add/componentversion
		parent := cmd.Parent()
		if parent.HasParent() {
			parent = parent.Parent()
		}
		parentCmd = parent.Name()

		dir = path.Join(dir, commandToDir(parent.CommandPath()))
		basename = commandToID(cmd.CommandPath()) + ".md"
	}

	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return fmt.Errorf("error creating command sub-directory: %w", err)
	}

	filename := filepath.Join(dir, basename)

	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			err = errors.Join(err, cerr)
		}
	}()

	// Builds the page frontmatter for this command.
	frontmatter := func() string {
		cmdName := commandToID(cmd.Name())
		title := strings.TrimSuffix(cmdName, path.Ext(cmdName))
		var url, name string
		if cmd.IsAdditionalHelpTopicCommand() {
			url = "docs/reference/ocm-cli/help/" + strings.ToLower(title) + "/"
			name = title
		} else if cmdName == "ocm-cli" {
			url = "docs/reference/ocm-cli/"
			title = "OCM CLI"
			name = "OCM CLI"
		} else if parentCmd == "ocm-cli" {
			url = "docs/reference/ocm-cli/" + strings.ToLower(title) + "/"
			name = title
		} else {
			url = "docs/reference/ocm-cli/" + parentCmd + "/" + strings.ToLower(title) + "/"
			name = fmt.Sprintf("%s %s", parentCmd, title)
		}
		return fmt.Sprintf(fmTmpl, title, name, url)
	}

	if _, err := io.WriteString(f, frontmatter()); err != nil {
		return err
	}

	if err := genMarkdownCustom(cmd, f, cmdToLink); err != nil {
		return err
	}

	return nil
}

// Renders the markdown body for a single command.
func genMarkdownCustom(cmd *cobra.Command, w io.Writer, cmdToLink map[string]string) error {
	cmd.InitDefaultHelpCmd()
	cmd.InitDefaultHelpFlag()

	buf := new(bytes.Buffer)
	name := cmd.CommandPath()
	linkHandler := func(path string) string {
		return cmdToLink[path]
	}

	if cmd.Runnable() || cmd.HasAvailableSubCommands() && name != "ocm" {
		buf.WriteString("### Usage\n\n")
		buf.WriteString(fmt.Sprintf("```\n%s\n```\n\n", useLine(cmd)))
	}

	if cmd.IsAvailableCommand() {
		if err := printOptions(buf, cmd); err != nil {
			return err
		}
	}

	if len(cmd.Long) > 0 {
		if name == "ocm" {
			buf.WriteString("### Introduction\n\n")
		} else {
			buf.WriteString("### Description\n\n")
		}
		_, desc := cobrautils.SubstituteCommandLinks(cmd.Long, cobrautils.FormatLinkWithHandler(linkHandler))
		buf.WriteString(desc + "\n\n")
	}

	if len(cmd.Example) > 0 {
		filtered := strings.ReplaceAll(cmd.Example, "<pre>\n", "")
		filtered = strings.ReplaceAll(filtered, "</pre>\n", "")
		buf.WriteString("### Examples\n\n")
		buf.WriteString(fmt.Sprintf("```\n%s\n```\n\n", filtered))
	}

	if hasSeeAlso(cmd) {
		header := cmd.HasHelpSubCommands() && cmd.HasAvailableSubCommands()
		buf.WriteString("### See Also\n\n")
		if cmd.HasParent() {
			header = true
			parent := cmd
			for parent.HasParent() {
				parent = parent.Parent()
				pname := parent.CommandPath()
				if parent.HasParent() {
					buf.WriteString(fmt.Sprintf("* [%s](%s)\t &mdash; %s\n", pname, linkHandler(parent.CommandPath()), parent.Short))
				}
			}
		}

		children := cmd.Commands()
		sort.Sort(byName(children))

		subheader := false
		for _, child := range children {
			if !child.IsAvailableCommand() || child.IsAdditionalHelpTopicCommand() {
				continue
			}
			if header && !subheader {
				buf.WriteString("\n\n##### Sub Commands\n\n")
				subheader = true
			}

			cname := name + " " + "<b>" + child.Name() + "</b>"
			buf.WriteString(fmt.Sprintf("* [%s](%s)\t &mdash; %s\n", cname, linkHandler(child.CommandPath()), child.Short))
		}
		buf.WriteString("\n")

		subheader = false
		for _, child := range children {
			if !child.IsAdditionalHelpTopicCommand() {
				continue
			}
			if header && !subheader {
				buf.WriteString("\n\n##### Additional Help Topics\n\n")
				subheader = true
			}
			cname := name + " " + "<b>" + child.Name() + "</b>"
			buf.WriteString(fmt.Sprintf("* [%s](%s)\t &mdash; %s\n", cname, cmdToLink[child.CommandPath()], child.Short))
		}
		if subheader {
			buf.WriteString("\n")
		}

	}

	_, err := buf.WriteTo(w)

	return err
}

// Writes the options section for a command, including inherited flags.
func printOptions(buf *bytes.Buffer, cmd *cobra.Command) error {
	flags := cmd.NonInheritedFlags()

	flags.SetOutput(buf)

	if flags.HasAvailableFlags() {
		buf.WriteString("### Options\n\n```\n")
		flags.PrintDefaults()
		buf.WriteString("```\n\n")
	}

	parentFlags := cmd.InheritedFlags()

	parentFlags.SetOutput(buf)

	if parentFlags.HasAvailableFlags() {
		buf.WriteString("### Options inherited from parent commands\n\n```\n")
		parentFlags.PrintDefaults()
		buf.WriteString("```\n\n")
	}

	return nil
}

// Builds a formatted usage line for the command.
func useLine(c *cobra.Command) string {
	useline := c.Use
	if !strings.Contains(useline, " ") {
		if c.HasAvailableLocalFlags() {
			useline += " [<options>]"
		}
		if c.HasAvailableSubCommands() {
			if c.Runnable() {
				useline += " [<sub command> ...]"
			} else {
				useline += " <sub command> ..."
			}
		}
	}

	if c.HasParent() {
		useline = c.Parent().CommandPath() + " " + useline
	}

	if c.DisableFlagsInUseLine {
		return useline
	}

	if c.HasAvailableFlags() && !strings.Contains(useline, "[<options>]") {
		useline += " [<options>]"
	}

	return useline
}

// Normalizes a command path into a stable page identifier.
func commandToID(command string) string {
	if command == "ocm" {
		return "ocm-cli"
	}
	return strings.TrimPrefix(strings.Replace(command, " ", "_", -1), "ocm_")
}

// Normalizes a command path into its directory name.
func commandToDir(command string) string {
	return strings.TrimPrefix(strings.Replace(command, " ", "-", -1), "ocm-")
}
