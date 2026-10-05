package internal

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
)

// TestEveryRegisteredUploaderHasAHandler builds a graph with each registered uploader
// type selecting a resource, so a type without a case in processResources fails here.
func TestEveryRegisteredUploaderHasAHandler(t *testing.T) {
	r := require.New(t)
	types := transferv1alpha1.UploaderTypes()
	r.NotEmpty(types)
	for _, typ := range types {
		obj, err := transferv1alpha1.Scheme.NewObject(typ)
		r.NoError(err, typ.String())
		r.NoError(json.Unmarshal([]byte(`{"match":"true","targetURL":"${\"https://example.com/x\"}"}`), obj), typ.String())

		desc := testDescriptor("ocm.software/test", "1.0.0", []descriptor.Resource{localBlobResource("blob", "1.0.0")}, nil)
		resolver := testResolverFor("ocm.software/test", "1.0.0", testOCIRepo("ghcr.io/source"), desc)
		roots := testTransferRoots("ocm.software/test", "1.0.0", testOCIRepo("ghcr.io/target"), resolver)
		_, err = BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{}, []transferv1alpha1.UploaderConfig{obj.(transferv1alpha1.UploaderConfig)})
		if err != nil {
			r.NotContains(err.Error(), "unsupported uploader config type", typ.String())
		}
	}
}
