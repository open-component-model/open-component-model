package credentialrepository_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialrepository"
	"ocm.software/open-component-model/bindings/go/runtime"
	wgetidentityv1 "ocm.software/open-component-model/bindings/go/wget/spec/identity/v1"
)

func TestRegisterConsumerIdentityTypeScheme(t *testing.T) {
	r := require.New(t)

	registry := credentialrepository.NewCredentialRepositoryRegistry(t.Context())

	identityScheme := runtime.NewScheme()
	wgetidentityv1.MustRegisterIdentityType(identityScheme)
	registry.RegisterConsumerIdentityTypeScheme(identityScheme)

	got := registry.GetConsumerIdentityTypeScheme()
	r.NotNil(got)

	for _, typ := range []runtime.Type{
		wgetidentityv1.VersionedType,
		wgetidentityv1.Type,
		runtime.NewVersionedType("HTTP", "v1"),
		runtime.NewUnversionedType("HTTP"),
		runtime.NewVersionedType("http", "v1"),
		runtime.NewUnversionedType("http"),
	} {
		r.True(got.IsRegistered(typ), "expected type %q to be registered", typ)

		canonical, ok := got.ResolveCanonicalType(typ)
		r.True(ok, "expected type %q to resolve canonically", typ)
		r.Equal(wgetidentityv1.VersionedType, canonical, "expected type %q to resolve to %q", typ, wgetidentityv1.VersionedType)
	}
}
