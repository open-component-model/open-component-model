package credentials

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func Test_nodeID_Identity(t *testing.T) {
	id := runtime.Identity{"type": "OCIRegistry", "hostname": "docker.io"}
	// Identity implements fmt.Stringer — nodeID should produce the same output as String().
	assert.Equal(t, id.String(), nodeID(id))
}

// nonStringerTyped is a minimal runtime.Typed that does not implement fmt.Stringer.
type nonStringerTyped struct {
	Type runtime.Type
}

func (n *nonStringerTyped) GetType() runtime.Type    { return n.Type }
func (n *nonStringerTyped) SetType(typ runtime.Type) { n.Type = typ }
func (n *nonStringerTyped) DeepCopyTyped() runtime.Typed {
	cp := *n
	return &cp
}

func Test_nodeID_NonStringer(t *testing.T) {
	typed := &nonStringerTyped{Type: runtime.NewVersionedType("Foo", "v1")}
	// nonStringerTyped does not implement fmt.Stringer — nodeID falls back to %v.
	result := nodeID(typed)
	assert.NotEmpty(t, result)
}

func Test_nodeID_Deterministic(t *testing.T) {
	id := runtime.Identity{"type": "OCIRegistry", "hostname": "docker.io"}
	// Same input must always produce the same node ID.
	assert.Equal(t, nodeID(id), nodeID(id))
}

func Test_typedMatch_IdentityExact(t *testing.T) {
	a := runtime.Identity{"type": "OCIRegistry", "hostname": "docker.io"}
	b := runtime.Identity{"type": "OCIRegistry", "hostname": "docker.io"}
	assert.True(t, typedMatch(a, b))
}

func Test_typedMatch_IdentityWildcard(t *testing.T) {
	a := runtime.Identity{"type": "OCIRegistry", "hostname": "docker.io", "path": "my-org/my-repo"}
	b := runtime.Identity{"type": "OCIRegistry", "hostname": "docker.io", "path": "my-org/*"}
	// a matches wildcard b
	assert.True(t, typedMatch(a, b))
	// b (wildcard) does not match specific a
	assert.False(t, typedMatch(b, a))
}

func Test_typedMatch_IdentityNoMatch(t *testing.T) {
	a := runtime.Identity{"type": "OCIRegistry", "hostname": "docker.io"}
	b := runtime.Identity{"type": "OCIRegistry", "hostname": "quay.io"}
	assert.False(t, typedMatch(a, b))
}

func Test_typedMatch_RawAgainstRaw(t *testing.T) {
	a := &runtime.Raw{}
	b := &runtime.Raw{}
	require.NoError(t, a.UnmarshalJSON([]byte(`{"type":"OCIRegistry/v1","hostname":"docker.io"}`)))
	require.NoError(t, b.UnmarshalJSON([]byte(`{"type":"OCIRegistry/v1","hostname":"docker.io"}`)))
	assert.True(t, typedMatch(a, b))
}

func Test_typedMatch_RawAgainstIdentity(t *testing.T) {
	id := runtime.Identity{"type": "OCIRegistry/v1", "hostname": "docker.io"}
	raw := &runtime.Raw{}
	require.NoError(t, raw.UnmarshalJSON([]byte(`{"type":"OCIRegistry/v1","hostname":"docker.io"}`)))
	assert.True(t, typedMatch(id, raw))
	assert.True(t, typedMatch(raw, id))
}

func Test_typedMatch_UnprojectableReturnsFalse(t *testing.T) {
	// A Raw without a JSON payload cannot be projected to Identity. The
	// matcher must surface this as a non-match rather than a panic so that
	// graph walks remain live.
	a := &runtime.Raw{Type: runtime.NewVersionedType("Foo", "v1")}
	b := &runtime.Raw{Type: runtime.NewVersionedType("Foo", "v1")}
	assert.NotPanics(t, func() { assert.False(t, typedMatch(a, b)) })
}

func Test_matchAnyNode_ExactMatch(t *testing.T) {
	dag := newSyncedDag()
	id := runtime.Identity{"type": "OCIRegistry", "hostname": "docker.io"}
	require.NoError(t, dag.addIdentity(id))

	vertex, err := dag.matchAnyNode(id)
	require.NoError(t, err)
	assert.Equal(t, nodeID(id), vertex.ID)
}

func Test_matchAnyNode_WildcardMatch(t *testing.T) {
	dag := newSyncedDag()
	wildcard := runtime.Identity{"type": "OCIRegistry", "hostname": "docker.io", "path": "my-org/*"}
	require.NoError(t, dag.addIdentity(wildcard))

	specific := runtime.Identity{"type": "OCIRegistry", "hostname": "docker.io", "path": "my-org/my-repo"}
	vertex, err := dag.matchAnyNode(specific)
	require.NoError(t, err)
	assert.Equal(t, nodeID(wildcard), vertex.ID)
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

	typed, ok := dag.getIdentity(nodeID(id))
	require.True(t, ok)
	assert.Equal(t, id, typed)
}

func Test_addIdentity_Idempotent(t *testing.T) {
	dag := newSyncedDag()
	id := runtime.Identity{"type": "OCIRegistry", "hostname": "docker.io"}
	require.NoError(t, dag.addIdentity(id))
	require.NoError(t, dag.addIdentity(id)) // second add is a no-op
	assert.Len(t, dag.dag.Vertices, 1)
}
