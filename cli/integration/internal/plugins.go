package internal

import (
	"fmt"
	"strings"

	"ocm.software/open-component-model/cli/cmd/plugins/list"
)

func CreatePluginComponentConstructors(name, version string) string {
	return fmt.Sprintf(`
---
name: %s
version: %s
provider:
  name: ocm.software
`, name, version)
}

func GeneratePluginReferences(name, version, description string, platforms []string) string {
	var s string

	s += fmt.Sprintf(`
  - name: %s
    version: %s
    componentName: %s
`, name, version, name)

	// Add labels if description or platforms are provided
	var labels string
	if description != "" {
		labels += fmt.Sprintf(`
          description: %s
`, description)
	}

	if len(platforms) > 0 {
		labels += `
          platforms:
`

		for _, platform := range platforms {
			labels += fmt.Sprintf(`
            - %s`, strings.TrimSpace(platform))
		}
	}

	if labels != "" {
		s += fmt.Sprintf(`
    labels:
      - name: %s
        value:
%s`, list.PluginInfoKey, labels)
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
