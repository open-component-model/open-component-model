package credentials_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/maven/spec/credentials"
	v1 "ocm.software/open-component-model/bindings/go/maven/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestScheme(t *testing.T) {
	for _, typ := range []runtime.Type{
		runtime.NewVersionedType(v1.MavenCredentialsType, v1.Version),
		runtime.NewUnversionedType(v1.MavenCredentialsType),
	} {
		obj, err := credentials.Scheme.NewObject(typ)
		require.NoError(t, err, typ.String())
		assert.IsType(t, &v1.MavenCredentials{}, obj, typ.String())
	}
}
