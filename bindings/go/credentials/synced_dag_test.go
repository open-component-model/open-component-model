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
	// match a path-bearing lookup on port 443. The port-scoped entry has more
	// attributes, so it must win deterministically over the hostname-only one.
	dag := newSyncedDag()
	hostnameOnly := runtime.Identity{"type": "OCIRegistry", "hostname": "ghcr.io"}
	portScoped := runtime.Identity{"type": "OCIRegistry", "hostname": "ghcr.io", "port": "443"}
	require.NoError(t, dag.addIdentity(hostnameOnly))
	require.NoError(t, dag.addIdentity(portScoped))

	lookup := runtime.Identity{"type": "OCIRegistry", "hostname": "ghcr.io", "port": "443", "path": "org/repo"}

	// Repeat to make the determinism explicit: map iteration order varies per run.
	for range 50 {
		vertex, err := dag.matchAnyNode(lookup)
		require.NoError(t, err)
		assert.Equal(t, portScoped.String(), vertex.ID)
	}
}

func Test_matchAnyNode_StableTieBreakOnEqualSpecificity(t *testing.T) {
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
