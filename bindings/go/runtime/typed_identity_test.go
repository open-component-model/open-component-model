package runtime_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// stubTyped is a minimal Typed used to exercise the projection without
// pulling in any other binding module.
type stubTyped struct {
	Type      runtime.Type `json:"type"`
	Hostname  string       `json:"hostname,omitempty"`
	Algorithm string       `json:"algorithm,omitempty"`
}

func (s *stubTyped) GetType() runtime.Type      { return s.Type }
func (s *stubTyped) SetType(t runtime.Type)     { s.Type = t }
func (s *stubTyped) DeepCopyTyped() runtime.Typed {
	c := *s
	return &c
}

// stubNested has a non-flat JSON shape to verify the strict projection.
type stubNested struct {
	Type   runtime.Type      `json:"type"`
	Nested map[string]string `json:"nested"`
}

func (s *stubNested) GetType() runtime.Type      { return s.Type }
func (s *stubNested) SetType(t runtime.Type)     { s.Type = t }
func (s *stubNested) DeepCopyTyped() runtime.Typed {
	c := *s
	return &c
}

func TestTypedToIdentity_NativeStruct(t *testing.T) {
	v := &stubTyped{
		Type:      runtime.NewVersionedType("Stub", "v1"),
		Hostname:  "registry.example.com",
		Algorithm: "RS256",
	}

	got, err := runtime.TypedToIdentity(v)
	require.NoError(t, err)

	assert.Equal(t, runtime.Identity{
		"type":      "Stub/v1",
		"hostname":  "registry.example.com",
		"algorithm": "RS256",
	}, got)
}

func TestTypedToIdentity_OmitsEmptyFields(t *testing.T) {
	v := &stubTyped{Type: runtime.NewVersionedType("Stub", "v1")}

	got, err := runtime.TypedToIdentity(v)
	require.NoError(t, err)

	assert.Equal(t, runtime.Identity{"type": "Stub/v1"}, got)
}

func TestTypedToIdentity_RawFromPlugin(t *testing.T) {
	payload := []byte(`{"type":"PluginX/v1","hostname":"plugin.example.com","scheme":"https","port":"443","path":"api"}`)
	raw := &runtime.Raw{}
	require.NoError(t, json.Unmarshal(payload, raw))

	got, err := runtime.TypedToIdentity(raw)
	require.NoError(t, err)

	assert.Equal(t, runtime.Identity{
		"type":     "PluginX/v1",
		"hostname": "plugin.example.com",
		"scheme":   "https",
		"port":     "443",
		"path":     "api",
	}, got)
}

func TestTypedToIdentity_IdentityRoundTrips(t *testing.T) {
	id := runtime.Identity{
		"type":     "Stub/v1",
		"hostname": "x",
		"path":     "p",
	}

	got, err := runtime.TypedToIdentity(id)
	require.NoError(t, err)
	assert.Equal(t, id, got)
}

func TestTypedToIdentity_RejectsNil(t *testing.T) {
	_, err := runtime.TypedToIdentity(nil)
	assert.Error(t, err)
}

func TestTypedToIdentity_RejectsNestedObject(t *testing.T) {
	v := &stubNested{
		Type:   runtime.NewVersionedType("Stub", "v1"),
		Nested: map[string]string{"k": "v"},
	}
	_, err := runtime.TypedToIdentity(v)
	assert.Error(t, err)
}

func TestTypedToIdentity_RejectsNonStringScalar(t *testing.T) {
	rawBytes := []byte(`{"type":"Stub/v1","port":443}`)
	raw := &runtime.Raw{}
	require.NoError(t, json.Unmarshal(rawBytes, raw))

	_, err := runtime.TypedToIdentity(raw)
	assert.Error(t, err)
}
