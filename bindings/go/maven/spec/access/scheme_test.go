package access_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/maven/spec/access"
	"ocm.software/open-component-model/bindings/go/maven/spec/access/v2alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestSchemeRegistersAliases(t *testing.T) {
	for _, typ := range []string{"maven/v2alpha1", "Maven/v2alpha1"} {
		t.Run(typ, func(t *testing.T) {
			parsed, err := runtime.TypeFromString(typ)
			require.NoError(t, err)
			obj, err := access.Scheme.NewObject(parsed)
			require.NoError(t, err)
			_, ok := obj.(*v2alpha1.Maven)
			assert.True(t, ok)
		})
	}
}

func TestSchemeRejectsOldTypes(t *testing.T) {
	for _, typ := range []string{"maven/v1", "Maven/v1", "maven", "Maven"} {
		t.Run(typ, func(t *testing.T) {
			parsed, err := runtime.TypeFromString(typ)
			require.NoError(t, err)
			_, err = access.Scheme.NewObject(parsed)
			require.Error(t, err)
		})
	}
}

func TestConsumerTypeConstant(t *testing.T) {
	assert.Equal(t, "MavenRepository", access.MavenRepositoryConsumerType)
}
