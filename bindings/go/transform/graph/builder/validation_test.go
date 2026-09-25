package builder

import (
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/transform/graph"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
)

func TestValidateTransformations_ReservedKeywords(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		// "environment" is the identifier of the CEL constant holding the
		// static environment. A transformation with this ID would overlap it,
		// and every expression referencing "environment" would then fail to
		// compile.
		{name: "environment is reserved", id: "environment", wantErr: true},
		{name: "spec is reserved", id: "spec", wantErr: true},
		{name: "regular id is accepted", id: "getResource", wantErr: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := require.New(t)

			err := ValidateTransformations(map[string]graph.Transformation{
				test.id: {GenericTransformation: v1alpha1.GenericTransformation{
					TransformationMeta: meta.TransformationMeta{ID: test.id},
				}},
			})
			if test.wantErr {
				r.ErrorContains(err, "reserved keyword")
			} else {
				r.NoError(err)
			}
		})
	}
}
