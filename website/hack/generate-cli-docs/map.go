package main

import (
	"fmt"
	"strings"
)

// Returns relref-based links for all supported CLI commands.
func buildCmdToLink(basePath string) map[string]string {
	basePath = strings.Trim(basePath, "/")

	commands := []string{
		"ocm add componentversions",
		"ocm add references",
		"ocm add resource-configuration",
		"ocm add resources",
		"ocm add routingslips",
		"ocm add source-configuration",
		"ocm add sources",
		"ocm add",
		"ocm attributes",
		"ocm credential-handling",
		"ocm check componentversions",
		"ocm check",
		"ocm clean cache",
		"ocm clean",
		"ocm configfile",
		"ocm create componentarchive",
		"ocm create rsakeypair",
		"ocm create transportarchive",
		"ocm create",
		"ocm describe artifacts",
		"ocm describe cache",
		"ocm describe package",
		"ocm describe plugins",
		"ocm describe",
		"ocm download artifacts",
		"ocm download cli",
		"ocm download componentversions",
		"ocm download resources",
		"ocm download",
		"ocm get artifacts",
		"ocm get componentversions",
		"ocm get config",
		"ocm get credentials",
		"ocm get plugins",
		"ocm get pubsub",
		"ocm get references",
		"ocm get resources",
		"ocm get routingslips",
		"ocm get sources",
		"ocm get verified",
		"ocm get",
		"ocm list componentversions",
		"ocm list",
		"ocm logging",
		"ocm oci-references",
		"ocm ocm-accessmethods",
		"ocm ocm-downloadhandlers",
		"ocm ocm-labels",
		"ocm ocm-pubsub",
		"ocm ocm-references",
		"ocm ocm-uploadhandlers",
		"ocm set pubsub",
		"ocm set",
		"ocm show tags",
		"ocm show versions",
		"ocm show",
		"ocm sign componentversions",
		"ocm sign hash",
		"ocm sign",
		"ocm toi-bootstrapping",
		"ocm transfer artifacts",
		"ocm transfer commontransportarchive",
		"ocm transfer componentarchive",
		"ocm transfer componentversions",
		"ocm transfer",
		"ocm verify componentversions",
		"ocm verify",
		"ocm",
	}

	cmdToLink := map[string]string{
		"ocm bootstrap configuration": "https://github.com/open-component-model/ocm/blob/main/docs/reference/ocm_bootstrap_configuration.md",
		"ocm bootstrap package":       "https://github.com/open-component-model/ocm/blob/main/docs/reference/ocm_bootstrap_package.md",
	}

	for _, command := range commands {
		docPath, ok := commandToDocPath(command)
		if !ok {
			continue
		}
		cmdToLink[command] = toRelref(fmt.Sprintf("%s/%s", basePath, docPath))
	}

	return cmdToLink
}

// Wraps a content path in a Hugo relref shortcode.
func toRelref(path string) string {
	return fmt.Sprintf("{{< relref %q >}}", path)
}

// Maps a CLI command to its markdown file path under docs/reference/ocm-cli.
func commandToDocPath(command string) (string, bool) {
	if command == "ocm" {
		return "_index.md", true
	}

	parts := strings.Fields(command)
	if len(parts) < 2 {
		return "", false
	}

	if isHelpTopic(parts[1]) && len(parts) == 2 {
		return fmt.Sprintf("help/%s.md", parts[1]), true
	}

	if len(parts) == 2 {
		return fmt.Sprintf("%s/_index.md", parts[1]), true
	}

	return fmt.Sprintf("%s/%s.md", parts[1], commandToID(command)), true
}

// Reports whether the given command name is rendered as a help topic page.
func isHelpTopic(name string) bool {
	return map[string]bool{
		"attributes":           true,
		"configfile":           true,
		"credential-handling":  true,
		"logging":              true,
		"oci-references":       true,
		"ocm-accessmethods":    true,
		"ocm-downloadhandlers": true,
		"ocm-labels":           true,
		"ocm-pubsub":           true,
		"ocm-references":       true,
		"ocm-uploadhandlers":   true,
		"toi-bootstrapping":    true,
	}[name]
}
