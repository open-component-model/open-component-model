package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestMustRegisterIdentityType(t *testing.T) {
	scheme := runtime.NewScheme()
	MustRegisterIdentityType(scheme)

	assert.True(t, scheme.IsRegistered(V1Alpha1Type))
	assert.True(t, scheme.IsRegistered(Type))

	obj, err := scheme.NewObject(V1Alpha1Type)
	require.NoError(t, err)
	_, ok := obj.(*GPGIdentity)
	assert.True(t, ok, "expected *GPGIdentity, got %T", obj)

	obj, err = scheme.NewObject(Type)
	require.NoError(t, err)
	_, ok = obj.(*GPGIdentity)
	assert.True(t, ok, "expected *GPGIdentity, got %T", obj)
}
