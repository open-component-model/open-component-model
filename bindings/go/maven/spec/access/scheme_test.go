package access_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/maven/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/maven/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestSchemeRegistersAliases(t *testing.T) {
	for _, typ := range []string{"Maven/v1", "Maven", "maven/v1", "maven"} {
		t.Run(typ, func(t *testing.T) {
			parsed, err := runtime.TypeFromString(typ)
			require.NoError(t, err)
			obj, err := access.Scheme.NewObject(parsed)
			require.NoError(t, err)
			_, ok := obj.(*v1.Maven)
			assert.True(t, ok)
		})
	}
}

func TestConsumerTypeConstant(t *testing.T) {
	assert.Equal(t, "MavenRepository", access.MavenRepositoryConsumerType)
}
