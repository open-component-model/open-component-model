package internal

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
)

func TestEveryRegisteredUploaderHasAHandler(t *testing.T) {
	r := require.New(t)
	types := transferv1alpha1.UploaderTypes()
	r.NotEmpty(types)
	for _, typ := range types {
		obj, err := transferv1alpha1.Scheme.NewObject(typ)
		r.NoError(err, typ.String())
		r.Contains(uploaderHandlers, reflect.TypeOf(obj), "no uploader handler for %s", typ)
	}
}
