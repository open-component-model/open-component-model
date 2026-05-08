package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestMustRegisterIdentityType(t *testing.T) {
	scheme := runtime.NewScheme()
	MustRegisterIdentityType(scheme)

	assert.True(t, scheme.IsRegistered(VersionedType))
	assert.True(t, scheme.IsRegistered(Type))

	obj, err := scheme.NewObject(VersionedType)
	require.NoError(t, err)
	_, ok := obj.(*Identity)
	assert.True(t, ok, "expected *Identity, got %T", obj)

	obj, err = scheme.NewObject(Type)
	require.NoError(t, err)
	_, ok = obj.(*Identity)
	assert.True(t, ok, "expected *Identity, got %T", obj)
}
