package componentversion

import (
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/constructor"
	"ocm.software/open-component-model/bindings/go/dag"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// newTypedAccessVertex builds a vertex holding a descriptor whose resource
// carries a typed access spec that is not registered in the descriptor scheme.
// This mirrors the output of the constructor for e.g. an "ociArtifact" access.
func newTypedAccessVertex() *dag.Vertex[string] {
	// Use a concrete typed value whose type is not registered in
	// descriptorv2.Scheme to reproduce the render failure.
	typed := &unregisteredAccess{Type: runtime.NewUnversionedType("ociArtifact"), ImageReference: "ghcr.io/example/image:1.0.0"}

	desc := &descriptor.Descriptor{}
	desc.Component.Name = "ocm.software/repro/oci-access"
	desc.Component.Version = "1.0.0"
	desc.Component.Provider.Name = "ocm.software"
	desc.Component.Resources = []descriptor.Resource{
		{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{Name: "image", Version: "1.0.0"},
			},
			Type:     "ociArtifact",
			Relation: descriptor.ExternalRelation,
			Access:   typed,
		},
	}

	return &dag.Vertex[string]{
		ID:         "name=ocm.software/repro/oci-access,version=1.0.0",
		Attributes: map[string]any{constructor.AttributeDescriptor: desc},
	}
}

// unregisteredAccess is a typed access spec that is intentionally not registered
// in descriptorv2.Scheme, matching how the constructor produces typed access
// specs for types provided by plugins.
type unregisteredAccess struct {
	Type           runtime.Type `json:"type"`
	ImageReference string       `json:"imageReference"`
}

func (a *unregisteredAccess) GetType() runtime.Type        { return a.Type }
func (a *unregisteredAccess) SetType(t runtime.Type)       { a.Type = t }
func (a *unregisteredAccess) DeepCopyTyped() runtime.Typed { c := *a; return &c }

func TestSerializeVertexToDescriptor_TypedUnregisteredAccess(t *testing.T) {
	r := require.New(t)

	v := newTypedAccessVertex()

	out, err := serializeVertexToDescriptor(v)
	r.NoError(err, "rendering a typed unregistered access must not fail")

	descV2, ok := out.(*descriptorv2.Descriptor)
	r.True(ok)
	r.Equal("ocm.software/repro/oci-access", descV2.Component.Name)
	r.Len(descV2.Component.Resources, 1)
	r.Equal("ociArtifact", descV2.Component.Resources[0].Access.Type.String())
}

func TestSerializeVertexToDescriptorTree_TypedUnregisteredAccess(t *testing.T) {
	r := require.New(t)

	v := newTypedAccessVertex()

	row, err := serializeVertexToDescriptorTree(v)
	r.NoError(err, "rendering a typed unregistered access must not fail")
	r.Equal("ocm.software/repro/oci-access", row.Component)
	r.Equal("1.0.0", row.Version)
}
