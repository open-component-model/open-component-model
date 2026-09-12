package credentialtyperepository_test

// Tests for merging built-in credential type schemes into the registry.
// These moved here from the credentialrepository package, which no longer owns a
// credential type scheme.

import (
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/plugin/internal/dummytype"
	dummyv1 "ocm.software/open-component-model/bindings/go/plugin/internal/dummytype/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialtyperepository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func newRegistry(t *testing.T) *credentialtyperepository.CredentialTypeRegistry {
	t.Helper()
	return credentialtyperepository.NewCredentialTypeRegistry(t.Context())
}

func TestRegister_BuiltinScheme(t *testing.T) {
	r := require.New(t)
	reg := newRegistry(t)

	r.NoError(reg.Register(dummytype.Scheme))

	scheme := reg.GetCredentialTypeScheme()
	r.True(scheme.IsRegistered(runtime.NewVersionedType(dummyv1.Type, dummyv1.Version)))
	r.True(scheme.IsRegistered(runtime.NewUnversionedType(dummyv1.Type)))
	r.True(scheme.IsRegistered(runtime.NewVersionedType(dummyv1.ShortType, dummyv1.Version)))
	r.True(scheme.IsRegistered(runtime.NewUnversionedType(dummyv1.ShortType)))
}

func TestRegister_MultipleSchemesAreMerged(t *testing.T) {
	r := require.New(t)
	reg := newRegistry(t)

	schemeA := runtime.NewScheme()
	schemeA.MustRegisterWithAlias(&runtime.Raw{}, runtime.NewVersionedType("CredA", "v1"))

	schemeB := runtime.NewScheme()
	schemeB.MustRegisterWithAlias(&runtime.Raw{}, runtime.NewVersionedType("CredB", "v1"))

	r.NoError(reg.Register(schemeA))
	r.NoError(reg.Register(schemeB))

	scheme := reg.GetCredentialTypeScheme()
	r.True(scheme.IsRegistered(runtime.NewVersionedType("CredA", "v1")))
	r.True(scheme.IsRegistered(runtime.NewVersionedType("CredB", "v1")))
}

// TestRegisterInternalCredentialTypeSchemeProvider covers the builtin path: the registry reads
// the types off the plugin instead of being handed a scheme.
func TestRegisterInternalCredentialTypeSchemeProvider(t *testing.T) {
	r := require.New(t)
	reg := newRegistry(t)

	r.NoError(reg.RegisterInternalCredentialTypeSchemeProvider(&schemeProviderPlugin{scheme: dummytype.Scheme}))

	scheme := reg.GetCredentialTypeScheme()
	r.True(scheme.IsRegistered(runtime.NewVersionedType(dummyv1.Type, dummyv1.Version)))
	r.True(scheme.IsRegistered(runtime.NewUnversionedType(dummyv1.Type)))
}

func TestRegisterInternalCredentialTypeSchemeProvider_NilPlugin(t *testing.T) {
	reg := newRegistry(t)
	require.ErrorContains(t, reg.RegisterInternalCredentialTypeSchemeProvider(nil), "nil credential type scheme provider")
}

// TestRegister_SameSchemeTwice covers more than one plugin of a binding declaring the same
// credential types, e.g. the wget input method and the wget resource repository.
func TestRegister_SameSchemeTwice(t *testing.T) {
	r := require.New(t)
	reg := newRegistry(t)

	r.NoError(reg.Register(dummytype.Scheme))
	r.NoError(reg.Register(dummytype.Scheme), "registering the same types again must be a no-op")
	r.True(reg.GetCredentialTypeScheme().IsRegistered(runtime.NewVersionedType(dummyv1.Type, dummyv1.Version)))
	r.True(reg.GetCredentialTypeScheme().IsRegistered(runtime.NewUnversionedType(dummyv1.Type)))
}

// TestRegister_AdditionalAlias verifies that a scheme contributing an additional alias for an
// already known prototype extends the registration instead of failing.
func TestRegister_AdditionalAlias(t *testing.T) {
	r := require.New(t)
	reg := newRegistry(t)

	versioned := runtime.NewVersionedType(dummyv1.Type, dummyv1.Version)
	alias := runtime.NewUnversionedType(dummyv1.Type)

	first := runtime.NewScheme()
	first.MustRegisterWithAlias(&dummyv1.Repository{}, versioned)
	second := runtime.NewScheme()
	second.MustRegisterWithAlias(&dummyv1.Repository{}, versioned, alias)

	r.NoError(reg.Register(first))
	r.NoError(reg.Register(second))
	r.True(reg.GetCredentialTypeScheme().IsRegistered(alias))
}

// TestRegister_ConflictingPrototype guards the case idempotency must not paper over: two bindings
// claiming the same type for different Go structs.
func TestRegister_ConflictingPrototype(t *testing.T) {
	r := require.New(t)
	reg := newRegistry(t)

	typ := runtime.NewVersionedType("CredA", "v1")

	first := runtime.NewScheme()
	first.MustRegisterWithAlias(&dummyv1.Repository{}, typ)
	second := runtime.NewScheme()
	second.MustRegisterWithAlias(&runtime.Raw{}, typ)

	r.NoError(reg.Register(first))
	r.ErrorContains(reg.Register(second), "already registered")
}

func TestRegister_NilScheme(t *testing.T) {
	require.NoError(t, newRegistry(t).Register(nil))
}

// TestRegisterInternalCredentialTypeSchemeProvider_ConflictNamesPlugin keeps the declaring plugin
// in the error, so a conflict points at the plugin that has to be fixed.
func TestRegisterInternalCredentialTypeSchemeProvider_ConflictNamesPlugin(t *testing.T) {
	r := require.New(t)
	reg := newRegistry(t)

	typ := runtime.NewVersionedType("CredA", "v1")

	first := runtime.NewScheme()
	first.MustRegisterWithAlias(&dummyv1.Repository{}, typ)
	second := runtime.NewScheme()
	second.MustRegisterWithAlias(&runtime.Raw{}, typ)

	r.NoError(reg.RegisterInternalCredentialTypeSchemeProvider(&schemeProviderPlugin{scheme: first}))
	err := reg.RegisterInternalCredentialTypeSchemeProvider(&schemeProviderPlugin{scheme: second})
	r.ErrorContains(err, "credentialtyperepository_test.schemeProviderPlugin")
}

// TestRegisterInternalCredentialTypeSchemeProvider_SamePluginTwice covers a binding that hands the
// same provider over twice, e.g. the wget resource repository registered as resource plugin and as
// digest processor.
func TestRegisterInternalCredentialTypeSchemeProvider_SamePluginTwice(t *testing.T) {
	r := require.New(t)
	reg := newRegistry(t)

	plugin := &schemeProviderPlugin{scheme: dummytype.Scheme}
	r.NoError(reg.RegisterInternalCredentialTypeSchemeProvider(plugin))
	r.NoError(reg.RegisterInternalCredentialTypeSchemeProvider(plugin))
}

// schemeProviderPlugin is a builtin plugin declaring the credential types it consumes.
type schemeProviderPlugin struct {
	scheme *runtime.Scheme
}

func (p *schemeProviderPlugin) GetCredentialTypeScheme() *runtime.Scheme {
	return p.scheme
}

// TestRegister_DefaultTypeIsNotItsOwnAlias pins the invariant the merge relies on: a scheme never
// lists a default type among its own aliases, so the candidate list built from type plus aliases
// holds no duplicate and the type is registered as default, not as an alias of itself.
func TestRegister_DefaultTypeIsNotItsOwnAlias(t *testing.T) {
	r := require.New(t)
	reg := newRegistry(t)

	typ := runtime.NewVersionedType("CredA", "v1")
	alias := runtime.NewUnversionedType("CredA")

	source := runtime.NewScheme()
	source.MustRegisterWithAlias(&dummyv1.Repository{}, typ, alias)
	r.Equal(map[runtime.Type][]runtime.Type{typ: {alias}}, source.GetTypes())

	r.NoError(reg.Register(source))

	scheme := reg.GetCredentialTypeScheme()
	r.Equal(map[runtime.Type][]runtime.Type{typ: {alias}}, scheme.GetTypes())

	canonical, ok := scheme.ResolveCanonicalType(alias)
	r.True(ok)
	r.Equal(typ, canonical)

	canonical, ok = scheme.ResolveCanonicalType(typ)
	r.True(ok)
	r.Equal(typ, canonical)
}
