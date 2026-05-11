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
