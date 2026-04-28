package credentials

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cfgRuntime "ocm.software/open-component-model/bindings/go/credentials/spec/config/runtime"
	v1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// logRecord captures a single slog record for test assertions.
type logRecord struct {
	Level   slog.Level
	Message string
	Attrs   map[string]string
}

// capturingHandler is a slog.Handler that captures log records for test assertions.
type capturingHandler struct {
	records *[]logRecord
}

func (h *capturingHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (h *capturingHandler) Handle(_ context.Context, r slog.Record) error {
	rec := logRecord{
		Level:   r.Level,
		Message: r.Message,
		Attrs:   make(map[string]string),
	}
	r.Attrs(func(a slog.Attr) bool {
		rec.Attrs[a.Key] = a.Value.String()
		return true
	})
	*h.records = append(*h.records, rec)
	return nil
}

func (h *capturingHandler) WithAttrs(_ []slog.Attr) slog.Handler  { return h }
func (h *capturingHandler) WithGroup(_ string) slog.Handler        { return h }

// withCaptureLogs installs a capturing slog handler for the duration of fn,
// then restores the previous default logger.
func withCaptureLogs(fn func()) []logRecord {
	var records []logRecord
	prev := slog.Default()
	slog.SetDefault(slog.New(&capturingHandler{records: &records}))
	defer slog.SetDefault(prev)
	fn()
	return records
}

func Test_isAccepted_ExactMatch(t *testing.T) {
	credType := runtime.NewVersionedType("HelmHTTPCredentials", "v1")
	accepted := []runtime.Type{credType}
	assert.True(t, isAccepted(nil, credType, accepted))
}

func Test_isAccepted_NoMatch(t *testing.T) {
	credType := runtime.NewVersionedType("WrongType", "v1")
	accepted := []runtime.Type{runtime.NewVersionedType("HelmHTTPCredentials", "v1")}
	assert.False(t, isAccepted(nil, credType, accepted))
}

func Test_isAccepted_AliasMatch(t *testing.T) {
	scheme := runtime.NewScheme()
	defaultType := runtime.NewVersionedType("HelmHTTPCredentials", "v1")
	alias := runtime.NewUnversionedType("HelmHTTPCredentials")
	scheme.MustRegisterWithAlias(&runtime.Raw{}, defaultType, alias)

	// Accepted list declares the versioned type, but the user configured the unversioned alias.
	accepted := []runtime.Type{defaultType}
	assert.True(t, isAccepted(scheme, alias, accepted),
		"unversioned alias should match versioned default via scheme resolution")

	// And vice versa: accepted declares alias, user configured default.
	accepted2 := []runtime.Type{alias}
	assert.True(t, isAccepted(scheme, defaultType, accepted2),
		"versioned default should match unversioned alias via scheme resolution")
}

func Test_isAccepted_NilScheme_FallsBackToExact(t *testing.T) {
	credType := runtime.NewVersionedType("HelmHTTPCredentials", "v1")
	alias := runtime.NewUnversionedType("HelmHTTPCredentials")
	accepted := []runtime.Type{credType}
	// Without a scheme, alias should NOT match
	assert.False(t, isAccepted(nil, alias, accepted))
}

func Test_isAccepted_WithAllowUnknown_NoFalsePositive(t *testing.T) {
	scheme := runtime.NewScheme(runtime.WithAllowUnknown())
	accepted := []runtime.Type{runtime.NewVersionedType("Foo", "v1")}
	credType := runtime.NewVersionedType("Bar", "v1")
	// Both types are unknown to the scheme. WithAllowUnknown causes NewObject
	// to return *Raw for any type — isAccepted must not treat them as aliases.
	assert.False(t, isAccepted(scheme, credType, accepted),
		"unrelated unknown types must not match even with WithAllowUnknown")
}

func Test_extractResolvable_MultipleTypedCredentials_FirstWins(t *testing.T) {
	credScheme := runtime.NewScheme()
	type1 := runtime.NewVersionedType("TypeA", "v1")
	type2 := runtime.NewVersionedType("TypeB", "v1")
	credScheme.MustRegisterWithAlias(&runtime.Raw{}, type1)
	credScheme.MustRegisterWithAlias(&runtime.Raw{}, type2)

	g := &Graph{
		credentialTypeSchemeProvider: &staticSchemeProvider{scheme: credScheme},
	}

	cred1 := &runtime.Raw{Type: type1, Data: []byte(`{"type":"TypeA/v1"}`)}
	cred2 := &runtime.Raw{Type: type2, Data: []byte(`{"type":"TypeB/v1"}`)}

	resolved, remaining, err := extractResolvable(context.Background(), g, []runtime.Typed{cred1, cred2})
	require.NoError(t, err)

	// First typed credential wins
	require.NotNil(t, resolved)
	assert.Equal(t, type1, resolved.GetType())

	// Second typed credential goes to remaining (not silently dropped)
	assert.Len(t, remaining, 1)
	assert.Equal(t, type2, remaining[0].GetType())
}

func Test_extractResolvable_DirectCredentialsMerge(t *testing.T) {
	g := &Graph{}

	dc1 := &runtime.Raw{
		Type: runtime.NewVersionedType(v1.CredentialsType, v1.Version),
		Data: []byte(`{"type":"Credentials/v1","properties":{"username":"admin"}}`),
	}
	dc2 := &runtime.Raw{
		Type: runtime.NewVersionedType(v1.CredentialsType, v1.Version),
		Data: []byte(`{"type":"Credentials/v1","properties":{"password":"secret"}}`),
	}

	resolved, remaining, err := extractResolvable(context.Background(), g, []runtime.Typed{dc1, dc2})
	require.NoError(t, err)
	assert.Empty(t, remaining)

	require.NotNil(t, resolved)
	dc, ok := resolved.(*v1.DirectCredentials)
	require.True(t, ok, "resolved should be DirectCredentials")
	assert.Equal(t, "admin", dc.Properties["username"])
	assert.Equal(t, "secret", dc.Properties["password"])
}

func Test_extractResolvable_DirectCredentials_NilProperties_NoPanic(t *testing.T) {
	g := &Graph{}

	// First DirectCredentials entry has no properties (nil map).
	dc1 := &runtime.Raw{
		Type: runtime.NewVersionedType(v1.CredentialsType, v1.Version),
		Data: []byte(`{"type":"Credentials/v1"}`),
	}
	// Second DirectCredentials entry has properties — would panic on maps.Copy
	// if the accumulator's Properties map is nil.
	dc2 := &runtime.Raw{
		Type: runtime.NewVersionedType(v1.CredentialsType, v1.Version),
		Data: []byte(`{"type":"Credentials/v1","properties":{"username":"admin"}}`),
	}

	resolved, remaining, err := extractResolvable(context.Background(), g, []runtime.Typed{dc1, dc2})
	require.NoError(t, err)
	assert.Empty(t, remaining)

	require.NotNil(t, resolved)
	dc, ok := resolved.(*v1.DirectCredentials)
	require.True(t, ok)
	assert.Equal(t, "admin", dc.Properties["username"])
}

// staticSchemeProvider is a test helper implementing CredentialTypeSchemeProvider.
type staticSchemeProvider struct {
	scheme *runtime.Scheme
}

func (s *staticSchemeProvider) GetCredentialTypeScheme() *runtime.Scheme {
	return s.scheme
}

func Test_validateConsumerIdentityTypes_UnparseableType(t *testing.T) {
	registry := NewIdentityTypeRegistry()
	g := &Graph{
		syncedDag:            newSyncedDag(),
		identityTypeRegistry: registry,
	}

	// Identity without a type set — ParseType will fail.
	identity := runtime.Identity{}
	config := &cfgRuntime.Config{
		Consumers: []cfgRuntime.Consumer{
			{Identities: []runtime.Identity{identity}},
		},
	}

	records := withCaptureLogs(func() {
		validateConsumerIdentityTypes(context.Background(), g, config)
	})

	require.Len(t, records, 1)
	assert.Equal(t, slog.LevelWarn, records[0].Level)
	assert.Equal(t, "consumer identity has unparseable type", records[0].Message)
}

func Test_validateConsumerIdentityTypes_UnregisteredType(t *testing.T) {
	registry := NewIdentityTypeRegistry()
	g := &Graph{
		syncedDag:            newSyncedDag(),
		identityTypeRegistry: registry,
	}

	identity := runtime.Identity{"type": "UnknownIdentity/v1"}
	config := &cfgRuntime.Config{
		Consumers: []cfgRuntime.Consumer{
			{Identities: []runtime.Identity{identity}},
		},
	}

	records := withCaptureLogs(func() {
		validateConsumerIdentityTypes(context.Background(), g, config)
	})

	require.Len(t, records, 1)
	assert.Equal(t, slog.LevelWarn, records[0].Level)
	assert.Equal(t, "consumer identity type not registered in scheme", records[0].Message)
	assert.Equal(t, "UnknownIdentity/v1", records[0].Attrs["type"])
}

func Test_validateConsumerIdentityTypes_CredentialNotAccepted(t *testing.T) {
	identityType := runtime.NewVersionedType("TestIdentity", "v1")
	acceptedCredType := runtime.NewVersionedType("AcceptedCred", "v1")
	wrongCredType := runtime.NewVersionedType("WrongCred", "v1")

	registry := NewIdentityTypeRegistry()
	require.NoError(t, registry.RegisterWithAcceptedCredentials(
		&runtime.Raw{},
		[]runtime.Type{identityType},
		[]runtime.Type{acceptedCredType},
	))

	g := &Graph{
		syncedDag:            newSyncedDag(),
		identityTypeRegistry: registry,
	}

	identity := runtime.Identity{"type": identityType.String()}
	config := &cfgRuntime.Config{
		Consumers: []cfgRuntime.Consumer{
			{
				Identities:  []runtime.Identity{identity},
				Credentials: []runtime.Typed{&runtime.Raw{Type: wrongCredType}},
			},
		},
	}

	records := withCaptureLogs(func() {
		validateConsumerIdentityTypes(context.Background(), g, config)
	})

	// Filter to warnings only (isAccepted may emit debug-level logs too).
	var warnings []logRecord
	for _, r := range records {
		if r.Level == slog.LevelWarn {
			warnings = append(warnings, r)
		}
	}

	require.Len(t, warnings, 1)
	assert.Equal(t, "credential type not accepted by identity type", warnings[0].Message)
	assert.Equal(t, "WrongCred/v1", warnings[0].Attrs["credentialType"])
	assert.Equal(t, "TestIdentity/v1", warnings[0].Attrs["identityType"])
}

func Test_validateConsumerIdentityTypes_AcceptedCredential_NoWarning(t *testing.T) {
	identityType := runtime.NewVersionedType("TestIdentity", "v1")
	acceptedCredType := runtime.NewVersionedType("AcceptedCred", "v1")

	registry := NewIdentityTypeRegistry()
	require.NoError(t, registry.RegisterWithAcceptedCredentials(
		&runtime.Raw{},
		[]runtime.Type{identityType},
		[]runtime.Type{acceptedCredType},
	))

	g := &Graph{
		syncedDag:            newSyncedDag(),
		identityTypeRegistry: registry,
	}

	identity := runtime.Identity{"type": identityType.String()}
	config := &cfgRuntime.Config{
		Consumers: []cfgRuntime.Consumer{
			{
				Identities:  []runtime.Identity{identity},
				Credentials: []runtime.Typed{&runtime.Raw{Type: acceptedCredType}},
			},
		},
	}

	records := withCaptureLogs(func() {
		validateConsumerIdentityTypes(context.Background(), g, config)
	})

	// No warnings should be emitted for accepted credential types.
	var warnings []logRecord
	for _, r := range records {
		if r.Level == slog.LevelWarn {
			warnings = append(warnings, r)
		}
	}
	assert.Empty(t, warnings)
}

func Test_validateConsumerIdentityTypes_NilRegistry_Noop(t *testing.T) {
	g := &Graph{
		syncedDag: newSyncedDag(),
	}
	config := &cfgRuntime.Config{
		Consumers: []cfgRuntime.Consumer{
			{Identities: []runtime.Identity{runtime.Identity{"type": "Anything/v1"}}},
		},
	}

	records := withCaptureLogs(func() {
		validateConsumerIdentityTypes(context.Background(), g, config)
	})

	assert.Empty(t, records)
}
