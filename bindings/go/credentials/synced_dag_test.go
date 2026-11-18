package credentials

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func Test_matchAnyNode_ExactMatch(t *testing.T) {
	dag := newSyncedDag()
	id := runtime.Identity{"type": "OCIRegistry", "hostname": "docker.io"}
	require.NoError(t, dag.addIdentity(id))

	vertex, err := dag.matchAnyNode(id)
	require.NoError(t, err)
	assert.Equal(t, id.String(), vertex.ID)
}

func Test_matchAnyNode_WildcardMatch(t *testing.T) {
	dag := newSyncedDag()
	wildcard := runtime.Identity{"type": "OCIRegistry", "hostname": "docker.io", "path": "my-org/*"}
	require.NoError(t, dag.addIdentity(wildcard))

	specific := runtime.Identity{"type": "OCIRegistry", "hostname": "docker.io", "path": "my-org/my-repo"}
	vertex, err := dag.matchAnyNode(specific)
	require.NoError(t, err)
	assert.Equal(t, wildcard.String(), vertex.ID)
}

func Test_matchAnyNode_NotFound(t *testing.T) {
	dag := newSyncedDag()
	id := runtime.Identity{"type": "OCIRegistry", "hostname": "docker.io"}

	_, err := dag.matchAnyNode(id)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrNoDirectCredentials)
}

func Test_matchAnyNode_PrefersMostSpecificEntry(t *testing.T) {
	// A hostname-only entry and the same host scoped to an explicit port both
	// match an https path-bearing lookup on port 443 (the well-known port lets
	// the scheme-less port-scoped entry and the hostname-only entry both match).
	// The port-scoped entry has more attributes, so it must win deterministically.
	dag := newSyncedDag()
	hostnameOnly := runtime.Identity{"type": "OCIRegistry", "hostname": "ghcr.io"}
	portScoped := runtime.Identity{"type": "OCIRegistry", "hostname": "ghcr.io", "port": "443"}
	require.NoError(t, dag.addIdentity(hostnameOnly))
	require.NoError(t, dag.addIdentity(portScoped))

	lookup := runtime.Identity{"type": "OCIRegistry", "hostname": "ghcr.io", "scheme": "https", "port": "443", "path": "org/repo"}
	// Guard the premise: the lookup must match both entries, otherwise the test
	// would not exercise selection between overlapping matches.
	require.True(t, lookup.Match(hostnameOnly), "lookup must match the hostname-only entry")
	require.True(t, lookup.Match(portScoped), "lookup must match the port-scoped entry")

	// Repeat to make the determinism explicit: map iteration order varies per run.
	for range 50 {
		vertex, err := dag.matchAnyNode(lookup)
		require.NoError(t, err)
		assert.Equal(t, portScoped.String(), vertex.ID)
	}
}

func Test_matchAnyNode_PrefersFewerPathWildcards(t *testing.T) {
	// Two path globs with the same attribute count both match. The glob with
	// fewer wildcards ("my-org/*") is more specific and must win over "*/*",
	// matching the credential-resolution "Example B2" documentation.
	dag := newSyncedDag()
	twoLevel := runtime.Identity{"type": "OCIRegistry", "hostname": "ghcr.io", "path": "*/*"}
	orgLevel := runtime.Identity{"type": "OCIRegistry", "hostname": "ghcr.io", "path": "my-org/*"}
	require.NoError(t, dag.addIdentity(twoLevel))
	require.NoError(t, dag.addIdentity(orgLevel))

	lookup := runtime.Identity{"type": "OCIRegistry", "hostname": "ghcr.io", "path": "my-org/repo"}

	for range 50 {
		vertex, err := dag.matchAnyNode(lookup)
		require.NoError(t, err)
		assert.Equal(t, orgLevel.String(), vertex.ID)
	}
}

func Test_matchAnyNode_StableLexicalTieBreak(t *testing.T) {
	// Two globs with the same attribute count and the same wildcard count both
	// match. Only the lexical tie-break on the identity string decides, so the
	// result must be stable: "my-org/*o" sorts before "my-org/r*".
	dag := newSyncedDag()
	suffix := runtime.Identity{"type": "OCIRegistry", "hostname": "ghcr.io", "path": "my-org/*o"}
	prefix := runtime.Identity{"type": "OCIRegistry", "hostname": "ghcr.io", "path": "my-org/r*"}
	require.NoError(t, dag.addIdentity(suffix))
	require.NoError(t, dag.addIdentity(prefix))

	lookup := runtime.Identity{"type": "OCIRegistry", "hostname": "ghcr.io", "path": "my-org/repo"}
	require.Less(t, suffix.String(), prefix.String(), "test premise: suffix glob sorts first")

	for range 50 {
		vertex, err := dag.matchAnyNode(lookup)
		require.NoError(t, err)
		assert.Equal(t, suffix.String(), vertex.ID)
	}
}

func Test_addIdentity_StoresAndRetrieves(t *testing.T) {
	dag := newSyncedDag()
	id := runtime.Identity{"type": "OCIRegistry", "hostname": "docker.io"}
	require.NoError(t, dag.addIdentity(id))

	stored, ok := dag.getIdentity(id.String())
	require.True(t, ok)
	assert.Equal(t, id, stored)
}

func Test_addIdentity_Idempotent(t *testing.T) {
	dag := newSyncedDag()
	id := runtime.Identity{"type": "OCIRegistry", "hostname": "docker.io"}
	require.NoError(t, dag.addIdentity(id))
	require.NoError(t, dag.addIdentity(id)) // second add is a no-op
	assert.Len(t, dag.dag.Vertices, 1)
}
