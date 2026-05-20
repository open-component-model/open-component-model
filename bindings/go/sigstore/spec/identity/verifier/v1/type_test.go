package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestMustRegisterIdentityType(t *testing.T) {
	r := require.New(t)
	scheme := runtime.NewScheme()
	MustRegisterIdentityType(scheme)

	assert.True(t, scheme.IsRegistered(VersionedType))
	assert.True(t, scheme.IsRegistered(Type))
	assert.True(t, scheme.IsRegistered(V1Alpha1Type))

	obj, err := scheme.NewObject(VersionedType)
	r.NoError(err)
	_, ok := obj.(*SigstoreVerifierIdentity)
	assert.True(t, ok, "expected *SigstoreVerifierIdentity, got %T", obj)

	obj, err = scheme.NewObject(Type)
	r.NoError(err)
	_, ok = obj.(*SigstoreVerifierIdentity)
	assert.True(t, ok, "expected *SigstoreVerifierIdentity, got %T", obj)

	obj, err = scheme.NewObject(V1Alpha1Type)
	r.NoError(err)
	_, ok = obj.(*SigstoreVerifierIdentity)
	assert.True(t, ok, "expected *SigstoreVerifierIdentity, got %T", obj)
}
