package credentialtyperepository_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialtyperepository"
	"ocm.software/open-component-model/bindings/go/runtime"
	wgetidentity "ocm.software/open-component-model/bindings/go/wget/spec/identity"
	wgetidentityv1 "ocm.software/open-component-model/bindings/go/wget/spec/identity/v1"
)

// consumerIdentitySchemeProviderPlugin is a builtin plugin that, on top of its credential
// types, declares the consumer identity types it resolves credentials for.
type consumerIdentitySchemeProviderPlugin struct {
	scheme         *runtime.Scheme
	identityScheme *runtime.Scheme
}

func (p *consumerIdentitySchemeProviderPlugin) GetCredentialTypeScheme() *runtime.Scheme {
	return p.scheme
}

func (p *consumerIdentitySchemeProviderPlugin) GetConsumerIdentityTypeScheme() *runtime.Scheme {
	return p.identityScheme
}

// The registry must collect the consumer identity types a provider declares so that the
// credential graph can canonicalize alias-typed config identities (HTTP -> Wget).
func TestRegisterInternalCredentialTypeSchemeProvider_ConsumerIdentityTypes(t *testing.T) {
	r := require.New(t)
	reg := credentialtyperepository.NewCredentialTypeRegistry(t.Context())

	plugin := &consumerIdentitySchemeProviderPlugin{identityScheme: wgetidentity.Scheme}
	r.NoError(reg.RegisterInternalCredentialTypeSchemeProvider(plugin))

	got := reg.GetConsumerIdentityTypeScheme()
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

	// Consumer identity types are not credential types and must not leak into that scheme.
	r.False(reg.GetCredentialTypeScheme().IsRegistered(wgetidentityv1.VersionedType))
}

// Both the wget input method and the wget resource repository declare the same identity
// scheme; registering them one after another must be a no-op, not a conflict.
func TestRegisterInternalCredentialTypeSchemeProvider_SameConsumerIdentitySchemeTwice(t *testing.T) {
	r := require.New(t)
	reg := credentialtyperepository.NewCredentialTypeRegistry(t.Context())

	plugin := &consumerIdentitySchemeProviderPlugin{identityScheme: wgetidentity.Scheme}
	r.NoError(reg.RegisterInternalCredentialTypeSchemeProvider(plugin))
	r.NoError(reg.RegisterInternalCredentialTypeSchemeProvider(plugin))
}

// A provider without the optional consumer identity scheme must register without error.
func TestRegisterInternalCredentialTypeSchemeProvider_NoConsumerIdentityTypes(t *testing.T) {
	r := require.New(t)
	reg := credentialtyperepository.NewCredentialTypeRegistry(t.Context())

	r.NoError(reg.RegisterInternalCredentialTypeSchemeProvider(&schemeProviderPlugin{scheme: nil}))

	got := reg.GetConsumerIdentityTypeScheme()
	r.NotNil(got)
	r.Empty(got.GetTypes())
}
