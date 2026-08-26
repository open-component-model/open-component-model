package access_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/github/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/github/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestScheme_TypeAliases(t *testing.T) {
	types := []runtime.Type{
		runtime.NewVersionedType(v1.Type, v1.Version),            // GitHub/v1
		runtime.NewUnversionedType(v1.Type),                      // GitHub
		runtime.NewVersionedType(v1.LegacyType, v1.Version),      // github/v1
		runtime.NewUnversionedType(v1.LegacyType),                // github
		runtime.NewVersionedType(v1.CamelLegacyType, v1.Version), // gitHub/v1
		runtime.NewUnversionedType(v1.CamelLegacyType),           // gitHub
	}
	for _, typ := range types {
		t.Run(typ.String(), func(t *testing.T) {
			obj, err := access.Scheme.NewObject(typ)
			require.NoError(t, err)
			assert.IsType(t, &v1.GitHub{}, obj)
		})
	}
}

func TestScheme_DecodeLegacyType(t *testing.T) {
	data := `{"type":"github","repoUrl":"https://github.com/open-component-model/ocm","commit":"0123456789abcdef0123456789abcdef01234567","ref":"refs/heads/main"}`

	gh := &v1.GitHub{}
	require.NoError(t, access.Scheme.Decode(strings.NewReader(data), gh))

	assert.Equal(t, "github", gh.Type.String())
	assert.Equal(t, "https://github.com/open-component-model/ocm", gh.RepoURL)
	assert.Equal(t, "0123456789abcdef0123456789abcdef01234567", gh.Commit)
	assert.Equal(t, "refs/heads/main", gh.Ref)
	assert.NoError(t, gh.Validate())
}
