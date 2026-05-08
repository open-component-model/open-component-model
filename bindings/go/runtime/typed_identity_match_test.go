package runtime_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// urlTyped models a typed identity carrying URL components, like
// OCIRegistryIdentity or the new runtime/spec/identity/v1.Identity.
type urlTyped struct {
	Type     runtime.Type `json:"type"`
	Hostname string       `json:"hostname,omitempty"`
	Scheme   string       `json:"scheme,omitempty"`
	Port     string       `json:"port,omitempty"`
	Path     string       `json:"path,omitempty"`
}

func (u *urlTyped) GetType() runtime.Type      { return u.Type }
func (u *urlTyped) SetType(t runtime.Type)     { u.Type = t }
func (u *urlTyped) DeepCopyTyped() runtime.Typed {
	c := *u
	return &c
}

// domainTyped models a domain-specific typed identity without URL fields,
// like RSAIdentity.
type domainTyped struct {
	Type      runtime.Type `json:"type"`
	Algorithm string       `json:"algorithm,omitempty"`
	Signature string       `json:"signature,omitempty"`
}

func (d *domainTyped) GetType() runtime.Type      { return d.Type }
func (d *domainTyped) SetType(t runtime.Type)     { d.Type = t }
func (d *domainTyped) DeepCopyTyped() runtime.Typed {
	c := *d
	return &c
}

func TestTypedMatch_NativeEqual(t *testing.T) {
	a := &urlTyped{
		Type:     runtime.NewVersionedType("OCIRegistry", "v1"),
		Hostname: "registry.example.com",
		Scheme:   "https",
		Path:     "v2",
	}
	b := &urlTyped{
		Type:     runtime.NewVersionedType("OCIRegistry", "v1"),
		Hostname: "registry.example.com",
		Scheme:   "https",
		Path:     "v2",
	}
	assert.True(t, runtime.TypedMatch(a, b))
}

func TestTypedMatch_DefaultPortViaURL(t *testing.T) {
	// https with implicit 443 must match https with explicit 443.
	a := &urlTyped{
		Type:     runtime.NewVersionedType("OCIRegistry", "v1"),
		Hostname: "registry.example.com",
		Scheme:   "https",
	}
	b := &urlTyped{
		Type:     runtime.NewVersionedType("OCIRegistry", "v1"),
		Hostname: "registry.example.com",
		Scheme:   "https",
		Port:     "443",
	}
	assert.True(t, runtime.TypedMatch(a, b))
}

func TestTypedMatch_PathGlob(t *testing.T) {
	a := &urlTyped{
		Type: runtime.NewVersionedType("OCIRegistry", "v1"),
		Path: "base/sub",
	}
	b := &urlTyped{
		Type: runtime.NewVersionedType("OCIRegistry", "v1"),
		Path: "base/*",
	}
	assert.True(t, runtime.TypedMatch(a, b))
}

func TestTypedMatch_CrossTypeFails(t *testing.T) {
	rsa := &domainTyped{
		Type:      runtime.NewVersionedType("RSA", "v1"),
		Algorithm: "RS256",
	}
	oci := &urlTyped{
		Type:     runtime.NewVersionedType("OCIRegistry", "v1"),
		Hostname: "registry.example.com",
		Scheme:   "https",
		Path:     "v2",
	}
	assert.False(t, runtime.TypedMatch(rsa, oci))
}

func TestTypedMatch_RawAgainstNative(t *testing.T) {
	payload := []byte(`{"type":"OCIRegistry/v1","hostname":"registry.example.com","scheme":"https","path":"v2"}`)
	raw := &runtime.Raw{}
	require.NoError(t, json.Unmarshal(payload, raw))

	native := &urlTyped{
		Type:     runtime.NewVersionedType("OCIRegistry", "v1"),
		Hostname: "registry.example.com",
		Scheme:   "https",
		Path:     "v2",
	}
	assert.True(t, runtime.TypedMatch(raw, native))
	assert.True(t, runtime.TypedMatch(native, raw))
}

func TestTypedMatch_RawAgainstRaw(t *testing.T) {
	a := &runtime.Raw{}
	b := &runtime.Raw{}
	require.NoError(t, json.Unmarshal([]byte(`{"type":"PluginX/v1","hostname":"x","scheme":"https"}`), a))
	require.NoError(t, json.Unmarshal([]byte(`{"type":"PluginX/v1","hostname":"x","scheme":"https","port":"443"}`), b))

	assert.True(t, runtime.TypedMatch(a, b))
}

func TestTypedMatch_SubsetCustomMatcher(t *testing.T) {
	sub := &urlTyped{
		Type:     runtime.NewVersionedType("OCIRegistry", "v1"),
		Hostname: "registry.example.com",
	}
	base := &urlTyped{
		Type:     runtime.NewVersionedType("OCIRegistry", "v1"),
		Hostname: "registry.example.com",
		Scheme:   "https",
		Path:     "v2",
	}
	assert.True(t, runtime.TypedMatch(sub, base, runtime.IdentityMatchingChainFn(runtime.IdentitySubset)))
}

func TestTypedMatch_NilSideFails(t *testing.T) {
	assert.False(t, runtime.TypedMatch(nil, &urlTyped{Type: runtime.NewVersionedType("X", "v1")}))
	assert.False(t, runtime.TypedMatch(&urlTyped{Type: runtime.NewVersionedType("X", "v1")}, nil))
}

func TestTypedMatch_DifferentTypeNamesFail(t *testing.T) {
	// Two structurally identical URL identities with different type names
	// must not match (the type attribute is part of the equality check).
	a := &urlTyped{
		Type:     runtime.NewVersionedType("OCIRegistry", "v1"),
		Hostname: "x",
	}
	b := &urlTyped{
		Type:     runtime.NewVersionedType("Identity", "v1"),
		Hostname: "x",
	}
	assert.False(t, runtime.TypedMatch(a, b))
}
