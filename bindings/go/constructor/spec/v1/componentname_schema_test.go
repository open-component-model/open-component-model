package v1_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	constructorv1 "ocm.software/open-component-model/bindings/go/constructor/spec/v1"
)

// componentNameSchemaCases pins the $defs.componentName grammar and the
// component-descriptors reservation as enforced by the embedded constructor JSON schema.
// See open-component-model/ocm-project#1422.
var componentNameSchemaCases = []struct {
	name  string
	valid bool
}{
	// accepted, including digit-led and digit-only segments
	{"acme.org/foo", true},
	{"1password.com/cli", true},
	{"acme.org/1", true},
	{"acme.org/v1/2", true},
	{"acme.org/a_b", true},
	{"github.com/open-component-model/ocm", true},
	// rejected
	{"acme.org/Foo", false},                     // uppercase segment
	{"Acme.org/foo", false},                     // uppercase label
	{"acme.org/foo-", false},                    // trailing separator
	{"acme.org/a_-b", false},                    // adjacent separators
	{"acme.org/a___b", false},                   // separator run not allowed
	{"acme-.org/x", false},                      // label trailing '-'
	{"acme.12/foo", false},                      // numeric top-level label
	{"acme/foo", false},                         // no alphabetic top-level label
	{"acme.org", false},                         // no path segment
	{"-a.org/x", false},                         // label starts with '-'
	{"acme.org/component-descriptors", false},   // reserved segment
	{"acme.org/component-descriptors/x", false}, // reserved segment in path
}

func constructorYAMLWithName(name string) []byte {
	return []byte(fmt.Sprintf(`components:
- name: %s
  version: 1.0.0
  provider:
    name: acme
  resources: []
`, name))
}

func TestComponentName_Schema(t *testing.T) {
	for _, tc := range componentNameSchemaCases {
		t.Run(tc.name, func(t *testing.T) {
			err := constructorv1.ValidateRawYAML(constructorYAMLWithName(tc.name))
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
