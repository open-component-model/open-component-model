package cache

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	credv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	identityv1 "ocm.software/open-component-model/bindings/go/oci/spec/identity/v1"
)

func TestScopeKey_Deterministic(t *testing.T) {
	id := &identityv1.OCIRegistryIdentity{Hostname: "ghcr.io", Path: "owner/repo"}
	creds := &credv1.OCICredentials{Username: "alice"}
	assert.Equal(t, ScopeKey(id, creds), ScopeKey(id, creds))
}

func TestScopeKey_FormatIs16Hex(t *testing.T) {
	got := ScopeKey(&identityv1.OCIRegistryIdentity{Hostname: "ghcr.io"}, nil)
	assert.Len(t, got, 16)
	assert.NotContains(t, got, "/")
	for _, r := range got {
		assert.True(t, strings.ContainsRune("0123456789abcdef", r), "non-hex char %q", r)
	}
}

func TestScopeKey_AnonymousIsStableAndDistinctFromAuthed(t *testing.T) {
	id := &identityv1.OCIRegistryIdentity{Hostname: "ghcr.io", Path: "owner/repo"}

	anon1 := ScopeKey(id, nil)
	anon2 := ScopeKey(id, &credv1.OCICredentials{})
	assert.Equal(t, anon1, anon2, "nil creds and empty creds must produce the same anonymous scope")

	authed := ScopeKey(id, &credv1.OCICredentials{Username: "alice"})
	assert.NotEqual(t, anon1, authed, "anonymous scope must not collide with an authenticated one")
}

func TestScopeKey_DistinctCredentialsDistinctScope(t *testing.T) {
	id := &identityv1.OCIRegistryIdentity{Hostname: "ghcr.io", Path: "owner/repo"}
	alice := ScopeKey(id, &credv1.OCICredentials{Username: "alice"})
	bob := ScopeKey(id, &credv1.OCICredentials{Username: "bob"})
	assert.NotEqual(t, alice, bob)
}

// TestScopeKey_TokensSharingPrefixDoNotCollapse guards the behaviour
// change from hashing only the first 16 bytes of a token to hashing
// the full token: two long tokens that share a 16-byte prefix must map
// to distinct scopes.
func TestScopeKey_TokensSharingPrefixDoNotCollapse(t *testing.T) {
	id := &identityv1.OCIRegistryIdentity{Hostname: "ghcr.io", Path: "owner/repo"}
	prefix := "0123456789abcdef" // 16 bytes shared prefix
	a := ScopeKey(id, &credv1.OCICredentials{AccessToken: prefix + "AAAAAAAAAAAAAAAA"})
	b := ScopeKey(id, &credv1.OCICredentials{AccessToken: prefix + "BBBBBBBBBBBBBBBB"})
	assert.NotEqual(t, a, b, "tokens sharing a 16-byte prefix must not collapse into one scope")
}

func TestScopeKey_AccessAndRefreshTokenDistinct(t *testing.T) {
	id := &identityv1.OCIRegistryIdentity{Hostname: "ghcr.io"}
	access := ScopeKey(id, &credv1.OCICredentials{AccessToken: "tok"})
	refresh := ScopeKey(id, &credv1.OCICredentials{RefreshToken: "tok"})
	// Same string, different field, but the discriminator is the token
	// value itself, so these collapse. Document that.
	assert.Equal(t, access, refresh)
}

func TestScopeKey_UsernameTakesPrecedenceOverTokens(t *testing.T) {
	id := &identityv1.OCIRegistryIdentity{Hostname: "ghcr.io"}
	withToken := ScopeKey(id, &credv1.OCICredentials{Username: "alice", AccessToken: "ignored"})
	usernameOnly := ScopeKey(id, &credv1.OCICredentials{Username: "alice"})
	assert.Equal(t, usernameOnly, withToken, "username must be the discriminator when present")
}

func TestScopeKey_IdentityComponentsAffectScope(t *testing.T) {
	base := ScopeKey(&identityv1.OCIRegistryIdentity{Hostname: "ghcr.io", Path: "owner/repo"}, nil)
	withPort := ScopeKey(&identityv1.OCIRegistryIdentity{Hostname: "ghcr.io", Port: "5000", Path: "owner/repo"}, nil)
	otherPath := ScopeKey(&identityv1.OCIRegistryIdentity{Hostname: "ghcr.io", Path: "owner/other"}, nil)

	assert.NotEqual(t, base, withPort)
	assert.NotEqual(t, base, otherPath)
}

func TestScopeKey_NilIdentityAndCreds(t *testing.T) {
	got := ScopeKey(nil, nil)
	assert.Len(t, got, 16)
}
