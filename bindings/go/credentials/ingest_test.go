package credentials

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"ocm.software/open-component-model/bindings/go/runtime"
)

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
