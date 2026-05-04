package credentials

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func Test_toIdentity_Identity(t *testing.T) {
	id := runtime.Identity{"type": "OCIRegistry", "hostname": "docker.io"}
	result, err := toIdentity(id)
	require.NoError(t, err)
	assert.Equal(t, id, result)
}

func Test_toIdentity_Nil(t *testing.T) {
	_, err := toIdentity(nil)
	require.Error(t, err)
}

func Test_toIdentity_NonIdentity(t *testing.T) {
	raw := &runtime.Raw{Type: runtime.NewVersionedType("Foo", "v1")}
	_, err := toIdentity(raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot convert *runtime.Raw to runtime.Identity")
}
