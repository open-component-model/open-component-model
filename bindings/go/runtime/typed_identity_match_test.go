package runtime_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// urlTyped models a typed identity carrying URL components, like
// OCIRegistryIdentity or runtime/spec/identity/v1.Identity.
type urlTyped struct {
	Type     runtime.Type `json:"type"`
	Hostname string       `json:"hostname,omitempty"`
	Scheme   string       `json:"scheme,omitempty"`
	Port     string       `json:"port,omitempty"`
	Path     string       `json:"path,omitempty"`
}

func (u *urlTyped) GetType() runtime.Type  { return u.Type }
func (u *urlTyped) SetType(t runtime.Type) { u.Type = t }
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

func (d *domainTyped) GetType() runtime.Type  { return d.Type }
func (d *domainTyped) SetType(t runtime.Type) { d.Type = t }
func (d *domainTyped) DeepCopyTyped() runtime.Typed {
	c := *d
	return &c
}

// rawJSON returns a Raw populated from the given canonical JSON payload.
func rawJSON(t *testing.T, payload string) *runtime.Raw {
	t.Helper()
	r := &runtime.Raw{}
	require.NoError(t, r.UnmarshalJSON([]byte(payload)))
	return r
}

// TestTypedMatch_AcrossRepresentations asserts representation-independence:
// the same logical identity carried as an Identity map, a typed struct, or a
// Raw JSON payload must match itself in every cross-pairing.
func TestTypedMatch_AcrossRepresentations(t *testing.T) {
	const payload = `{"type":"OCIRegistry/v1","hostname":"registry.example.com","scheme":"https","path":"v2"}`

	reps := map[string]func() runtime.Typed{
		"Identity": func() runtime.Typed {
			return runtime.Identity{
				"type":     "OCIRegistry/v1",
				"hostname": "registry.example.com",
				"scheme":   "https",
				"path":     "v2",
			}
		},
		"Struct": func() runtime.Typed {
			return &urlTyped{
				Type:     runtime.NewVersionedType("OCIRegistry", "v1"),
				Hostname: "registry.example.com",
				Scheme:   "https",
				Path:     "v2",
			}
		},
		"Raw": func() runtime.Typed { return rawJSON(t, payload) },
	}

	for nameA, makeA := range reps {
		for nameB, makeB := range reps {
			t.Run(nameA+"_vs_"+nameB, func(t *testing.T) {
				assert.True(t, runtime.TypedMatch(makeA(), makeB()))
			})
		}
	}
}

// TestTypedMatch_RejectsMismatch covers the default chain's negative cases:
// any meaningful mismatch (hostname, version, semantic kind) must yield false.
func TestTypedMatch_RejectsMismatch(t *testing.T) {
	tests := map[string]struct {
		a, b runtime.Typed
	}{
		"different hostname": {
			a: runtime.Identity{"type": "OCIRegistry", "hostname": "docker.io"},
			b: runtime.Identity{"type": "OCIRegistry", "hostname": "quay.io"},
		},
		"different type names with same fields": {
			a: &urlTyped{Type: runtime.NewVersionedType("OCIRegistry", "v1"), Hostname: "x"},
			b: &urlTyped{Type: runtime.NewVersionedType("Identity", "v1"), Hostname: "x"},
		},
		"semantic cross-type (RSA vs OCI)": {
			a: &domainTyped{Type: runtime.NewVersionedType("RSA", "v1"), Algorithm: "RS256"},
			b: &urlTyped{
				Type:     runtime.NewVersionedType("OCIRegistry", "v1"),
				Hostname: "registry.example.com",
				Scheme:   "https",
				Path:     "v2",
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.False(t, runtime.TypedMatch(tt.a, tt.b))
		})
	}
}

// TestTypedMatch_UnmatchableInputs covers inputs that cannot be projected.
// All such cases must return false without panicking — graph walks rely on
// liveness even when an opaque value flows through.
func TestTypedMatch_UnmatchableInputs(t *testing.T) {
	valid := &urlTyped{Type: runtime.NewVersionedType("X", "v1")}
	emptyRaw := &runtime.Raw{Type: runtime.NewVersionedType("Foo", "v1")}

	tests := map[string]struct {
		a, b runtime.Typed
	}{
		"nil left":            {a: nil, b: valid},
		"nil right":           {a: valid, b: nil},
		"nil both":            {a: nil, b: nil},
		"unprojectable left":  {a: emptyRaw, b: valid},
		"unprojectable right": {a: valid, b: emptyRaw},
		"unprojectable both":  {a: emptyRaw, b: emptyRaw},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				assert.False(t, runtime.TypedMatch(tt.a, tt.b))
			})
		})
	}
}

// TestTypedMatch_PathGlob covers path-attribute glob semantics from the
// default chain, including the asymmetric "specific matches wildcard but
// wildcard does not match specific" behavior.
func TestTypedMatch_PathGlob(t *testing.T) {
	specific := runtime.Identity{
		"type":     "OCIRegistry",
		"hostname": "docker.io",
		"path":     "my-org/my-repo",
	}
	wildcard := runtime.Identity{
		"type":     "OCIRegistry",
		"hostname": "docker.io",
		"path":     "my-org/*",
	}

	t.Run("specific against wildcard matches", func(t *testing.T) {
		assert.True(t, runtime.TypedMatch(specific, wildcard))
	})
	t.Run("wildcard against specific does not match", func(t *testing.T) {
		assert.False(t, runtime.TypedMatch(wildcard, specific))
	})
}

// TestTypedMatch_DefaultPortViaURL covers the URL default-port semantics:
// https with implicit 443 must match https with explicit 443.
func TestTypedMatch_DefaultPortViaURL(t *testing.T) {
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

// TestTypedMatch_SubsetCustomMatcher exercises the custom-matcher path:
// passing IdentitySubset as a chain function activates subset semantics
// instead of the default chain.
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
