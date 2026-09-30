package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestMustRegisterIdentityType(t *testing.T) {
	scheme := runtime.NewScheme()
	MustRegisterIdentityType(scheme)

	for _, typ := range []runtime.Type{VersionedType, Type} {
		t.Run(typ.String(), func(t *testing.T) {
			r := require.New(t)
			r.True(scheme.IsRegistered(typ))
			obj, err := scheme.NewObject(typ)
			r.NoError(err)
			r.IsType(&TSAIdentity{}, obj)
		})
	}
}
