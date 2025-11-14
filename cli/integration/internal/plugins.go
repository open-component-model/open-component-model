package internal

import (
	"fmt"
	"strings"

	"ocm.software/open-component-model/cli/cmd/plugins/list"
)

func CreatePluginComponentConstructors(info list.PluginInfo) string {
	return fmt.Sprintf(`
---
name: %s
version: %s
provider:
  name: ocm.software
`, info.Name, info.Version)
}

func GeneratePluginReferences(plugin list.PluginInfo) string {
	var s string

	s += fmt.Sprintf(`
  - name: %s
    version: %s
    componentName: %s
`, plugin.Name, plugin.Version, plugin.Name)

	// Add labels if description or platforms are provided
	var labels string
	if plugin.Description != "" {
		labels += fmt.Sprintf(`
          description: %s
`, plugin.Description)
	}

	if len(plugin.Platforms) > 0 {
		labels += `
          platforms:
`

		for _, platform := range plugin.Platforms {
			labels += fmt.Sprintf(`
            - %s`, strings.TrimSpace(platform))
		}
	}

	if labels != "" {
		s += fmt.Sprintf(`
    labels:
      - name: ocm.software/pluginInfo
        value:
%s`, labels)
	}

	return s
}

func CreatePluginRegistryConstructor(component, version, references string) string {
	return fmt.Sprintf(`
name: %s
version: %s 
provider:
  name: ocm.software
labels:
  - name: category
    value: plugin-registry
  - name: registry
    value: official
  - name: description
    value: Official OCM plugin registry

componentReferences:
%s
`, component, version, references)
}
