// Step 2: Component Descriptors
//
// What you'll learn:
//   - Building a component descriptor with resources, sources, and references
//   - Using ExtraIdentity to distinguish platform-specific resources
//   - Adding labels to components and resources
//   - Creating and inspecting inter-component references
//
// A component descriptor is the metadata heart of OCM. It describes what a
// component version contains (resources), where it came from (sources), and
// what it depends on (references). In the next steps you'll store these
// descriptors in repositories and sign them — but first you need to know how
// to build one.

package examples

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// TestExample_CreateDescriptor demonstrates building a complete component
// descriptor with resources, sources, and references.
func TestExample_CreateDescriptor(t *testing.T) {
	r := require.New(t)

	desc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			Provider: descriptor.Provider{Name: "acme.org"},
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "acme.org/my-app",
					Version: "1.0.0",
				},
			},
			Resources: []descriptor.Resource{
				{
					ElementMeta: descriptor.ElementMeta{
						ObjectMeta: descriptor.ObjectMeta{Name: "my-image", Version: "1.0.0"},
					},
					Type:     "ociImage",
					Relation: descriptor.ExternalRelation,
				},
			},
			Sources: []descriptor.Source{
				{
					ElementMeta: descriptor.ElementMeta{
						ObjectMeta: descriptor.ObjectMeta{Name: "source-repo", Version: "1.0.0"},
					},
					Type: "git",
				},
			},
			References: []descriptor.Reference{
				{
					ElementMeta: descriptor.ElementMeta{
						ObjectMeta: descriptor.ObjectMeta{Name: "backend-ref", Version: "2.0.0"},
					},
					Component: "acme.org/backend",
				},
			},
		},
	}

	r.Equal("acme.org/my-app", desc.Component.Name)
	r.Equal("1.0.0", desc.Component.Version)
	r.Equal("acme.org", desc.Component.Provider.Name)
	r.Len(desc.Component.Resources, 1)
	r.Len(desc.Component.Sources, 1)
	r.Len(desc.Component.References, 1)
}

// TestExample_ResourceIdentity shows how ExtraIdentity fields extend the
// identity of a resource beyond name and version.
func TestExample_ResourceIdentity(t *testing.T) {
	r := require.New(t)

	res := descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{Name: "platform-binary", Version: "1.0.0"},
			ExtraIdentity: runtime.Identity{
				"architecture": "amd64",
				"os":           "linux",
			},
		},
		Type:     "executable",
		Relation: descriptor.LocalRelation,
	}

	identity := res.ToIdentity()

	r.Equal("platform-binary", identity["name"])
	r.Equal("1.0.0", identity["version"])
	r.Equal("amd64", identity["architecture"])
	r.Equal("linux", identity["os"])
}

// TestExample_DescriptorWithLabels demonstrates adding labels to both
// components and resources.
func TestExample_DescriptorWithLabels(t *testing.T) {
	r := require.New(t)

	desc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			Provider: descriptor.Provider{Name: "acme.org"},
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "acme.org/labelled",
					Version: "1.0.0",
					Labels: []descriptor.Label{
						{Name: "env", Value: json.RawMessage(`"production"`)},
					},
				},
			},
			Resources: []descriptor.Resource{
				{
					ElementMeta: descriptor.ElementMeta{
						ObjectMeta: descriptor.ObjectMeta{
							Name:    "chart",
							Version: "1.0.0",
							Labels: []descriptor.Label{
								{Name: "deploy-order", Value: json.RawMessage(`1`)},
							},
						},
					},
					Type:     "helmChart",
					Relation: descriptor.LocalRelation,
				},
			},
		},
	}

	r.Len(desc.Component.Labels, 1)
	r.Equal("env", desc.Component.Labels[0].Name)

	r.Len(desc.Component.Resources[0].Labels, 1)
	r.Equal("deploy-order", desc.Component.Resources[0].Labels[0].Name)
}

// TestExample_ComponentReferences demonstrates creating inter-component
// references and inspecting their identity.
func TestExample_ComponentReferences(t *testing.T) {
	r := require.New(t)

	ref := descriptor.Reference{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{Name: "db-ref", Version: "3.0.0"},
		},
		Component: "acme.org/database",
	}

	// ToIdentity returns the reference's own identity (name-based).
	refIdentity := ref.ToIdentity()
	r.Equal("db-ref", refIdentity["name"])
	r.Equal("3.0.0", refIdentity["version"])

	// ToComponentIdentity returns the referenced component's identity.
	compIdentity := ref.ToComponentIdentity()
	r.Equal("acme.org/database", compIdentity["name"])
	r.Equal("3.0.0", compIdentity["version"])
}
