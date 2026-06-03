package credentialtype_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/plugin/internal/dummytype"
	dummyv1 "ocm.software/open-component-model/bindings/go/plugin/internal/dummytype/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialtype"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestNewRegistry(t *testing.T) {
	r := require.New(t)
	reg := credentialtype.NewRegistry()
	r.NotNil(reg)
	scheme := reg.GetCredentialTypeScheme()
	r.NotNil(scheme)
	r.Empty(scheme.GetTypes())
}

func TestRegister(t *testing.T) {
	r := require.New(t)
	reg := credentialtype.NewRegistry()

	reg.Register(dummytype.Scheme)

	scheme := reg.GetCredentialTypeScheme()
	r.True(scheme.IsRegistered(runtime.NewVersionedType(dummyv1.Type, dummyv1.Version)))
	r.True(scheme.IsRegistered(runtime.NewUnversionedType(dummyv1.Type)))
	r.True(scheme.IsRegistered(runtime.NewVersionedType(dummyv1.ShortType, dummyv1.Version)))
	r.True(scheme.IsRegistered(runtime.NewUnversionedType(dummyv1.ShortType)))
}

func TestRegister_MultipleSchemesAreMerged(t *testing.T) {
	r := require.New(t)
	reg := credentialtype.NewRegistry()

	schemeA := runtime.NewScheme()
	schemeA.MustRegisterWithAlias(&runtime.Raw{}, runtime.NewVersionedType("CredA", "v1"))

	schemeB := runtime.NewScheme()
	schemeB.MustRegisterWithAlias(&runtime.Raw{}, runtime.NewVersionedType("CredB", "v1"))

	reg.Register(schemeA)
	reg.Register(schemeB)

	scheme := reg.GetCredentialTypeScheme()
	r.True(scheme.IsRegistered(runtime.NewVersionedType("CredA", "v1")))
	r.True(scheme.IsRegistered(runtime.NewVersionedType("CredB", "v1")))
}
