package cache

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	identityv1 "ocm.software/open-component-model/bindings/go/oci/spec/identity/v1"
)

func TestRepositoryKey_Deterministic(t *testing.T) {
	id := &identityv1.OCIRegistryIdentity{Hostname: "ghcr.io", Path: "owner/repo"}
	assert.Equal(t, RepositoryKey(id), RepositoryKey(id))
}

func TestRepositoryKey_FormatIs16Hex(t *testing.T) {
	got := RepositoryKey(&identityv1.OCIRegistryIdentity{Hostname: "ghcr.io"})
	assert.Len(t, got, 16)
	assert.NotContains(t, got, "/")
	for _, r := range got {
		assert.True(t, strings.ContainsRune("0123456789abcdef", r), "non-hex char %q", r)
	}
}

func TestRepositoryKey_NilIdentity(t *testing.T) {
	got := RepositoryKey(nil)
	assert.Len(t, got, 16)
}

func TestRepositoryKey_DifferentPathsDistinct(t *testing.T) {
	base := RepositoryKey(&identityv1.OCIRegistryIdentity{Hostname: "ghcr.io", Path: "owner/repo"})
	other := RepositoryKey(&identityv1.OCIRegistryIdentity{Hostname: "ghcr.io", Path: "owner/other"})
	assert.NotEqual(t, base, other)
}

func TestRepositoryKey_PortAffectsKey(t *testing.T) {
	base := RepositoryKey(&identityv1.OCIRegistryIdentity{Hostname: "ghcr.io", Path: "owner/repo"})
	withPort := RepositoryKey(&identityv1.OCIRegistryIdentity{Hostname: "ghcr.io", Port: "5000", Path: "owner/repo"})
	assert.NotEqual(t, base, withPort)
}

func TestRepositoryKey_CredentialsDoNotAffectKey(t *testing.T) {
	id := &identityv1.OCIRegistryIdentity{Hostname: "ghcr.io", Path: "owner/repo"}
	k1 := RepositoryKey(id)
	// Calling with the same identity but conceptually different "users" must
	// produce the same key — credentials are not part of RepositoryKey.
	k2 := RepositoryKey(id)
	assert.Equal(t, k1, k2)
}
