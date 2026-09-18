package graph

import (
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
)

func TestTransformation_DisplayName(t *testing.T) {
	r := require.New(t)

	tr := &Transformation{
		GenericTransformation: v1alpha1.GenericTransformation{
			TransformationMeta: meta.TransformationMeta{
				Type: runtime.Type{Name: "AddComponentVersion"},
				ID:   "tiffkl6vme77t4",
			},
		},
	}
	r.Equal("tiffkl6vme77t4 [AddComponentVersion]", tr.DisplayName())

	tr.Label = "my-app@1.0.0 [Upload]"
	r.Equal("my-app@1.0.0 [Upload]", tr.DisplayName())
}
