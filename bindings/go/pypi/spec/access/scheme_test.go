package access_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/pypi/spec/access"
	"ocm.software/open-component-model/bindings/go/pypi/spec/access/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestSchemeRegistersAliases(t *testing.T) {
	for _, typ := range []string{"pypi/v1alpha1", "PyPI/v1alpha1"} {
		t.Run(typ, func(t *testing.T) {
			parsed, err := runtime.TypeFromString(typ)
			require.NoError(t, err)
			obj, err := access.Scheme.NewObject(parsed)
			require.NoError(t, err)
			_, ok := obj.(*v1alpha1.PyPI)
			assert.True(t, ok)
		})
	}
}

func TestSchemeRejectsOtherTypes(t *testing.T) {
	for _, typ := range []string{"pypi/v1", "PyPI/v1", "pypi", "PyPI"} {
		t.Run(typ, func(t *testing.T) {
			parsed, err := runtime.TypeFromString(typ)
			require.NoError(t, err)
			_, err = access.Scheme.NewObject(parsed)
			require.Error(t, err)
		})
	}
}

func TestConsumerTypeConstant(t *testing.T) {
	assert.Equal(t, "PyPIRepository", access.PyPIRepositoryConsumerType)
}
