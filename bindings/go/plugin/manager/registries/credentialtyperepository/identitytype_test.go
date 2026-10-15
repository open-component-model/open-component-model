package credentialtyperepository_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialtyperepository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// exampleIdentity is a local consumer-identity type used only to exercise the
// registry's alias canonicalization. It deliberately does not import a concrete
// identity from another binding (e.g. wget), which the layering rules disallow
// (see depguard in golangci.yml); its canonical+alias shape mirrors a real one.
type exampleIdentity struct {
	Type runtime.Type `json:"type"`
}

func (e *exampleIdentity) GetType() runtime.Type        { return e.Type }
func (e *exampleIdentity) SetType(t runtime.Type)       { e.Type = t }
func (e *exampleIdentity) DeepCopyTyped() runtime.Typed { cp := *e; return &cp }

var (
	exampleVersionedType   = runtime.NewVersionedType("Example", "v1")
	exampleUnversionedType = runtime.NewUnversionedType("Example")
)

// newExampleIdentityScheme registers Example/v1 as the canonical type with its
// unversioned form plus "Alias"/"alias" (versioned and unversioned) aliases, so
// alias-to-canonical resolution can be asserted.
func newExampleIdentityScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	scheme.MustRegisterWithAlias(&exampleIdentity{},
		exampleVersionedType,
		exampleUnversionedType,
		runtime.NewVersionedType("Alias", "v1"),
		runtime.NewUnversionedType("Alias"),
		runtime.NewVersionedType("alias", "v1"),
		runtime.NewUnversionedType("alias"),
	)
	return scheme
}

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
// credential graph can canonicalize alias-typed config identities (Alias -> Example).
func TestRegisterInternalCredentialTypeSchemeProvider_ConsumerIdentityTypes(t *testing.T) {
	r := require.New(t)
	reg := credentialtyperepository.NewCredentialTypeRegistry(t.Context())

	plugin := &consumerIdentitySchemeProviderPlugin{identityScheme: newExampleIdentityScheme()}
	r.NoError(reg.RegisterInternalCredentialTypeSchemeProvider(plugin))

	got := reg.GetConsumerIdentityTypeScheme()
	r.NotNil(got)

	for _, typ := range []runtime.Type{
		exampleVersionedType,
		exampleUnversionedType,
		runtime.NewVersionedType("Alias", "v1"),
		runtime.NewUnversionedType("Alias"),
		runtime.NewVersionedType("alias", "v1"),
		runtime.NewUnversionedType("alias"),
	} {
		r.True(got.IsRegistered(typ), "expected type %q to be registered", typ)

		canonical, ok := got.ResolveCanonicalType(typ)
		r.True(ok, "expected type %q to resolve canonically", typ)
		r.Equal(exampleVersionedType, canonical, "expected type %q to resolve to %q", typ, exampleVersionedType)
	}

	// Consumer identity types are not credential types and must not leak into that scheme.
	r.False(reg.GetCredentialTypeScheme().IsRegistered(exampleVersionedType))
}

// A provider may declare the same consumer identity scheme on behalf of several
// components (e.g. an input method and a resource repository); registering it one
// after another must be a no-op, not a conflict.
func TestRegisterInternalCredentialTypeSchemeProvider_SameConsumerIdentitySchemeTwice(t *testing.T) {
	r := require.New(t)
	reg := credentialtyperepository.NewCredentialTypeRegistry(t.Context())

	plugin := &consumerIdentitySchemeProviderPlugin{identityScheme: newExampleIdentityScheme()}
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
