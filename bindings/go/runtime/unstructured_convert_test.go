package runtime_test

import (
	"encoding/json"
	"maps"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// typedBase supplies GetType/SetType for mechanical test hosts; each host only adds DeepCopyTyped.
type typedBase struct {
	Type runtime.Type `json:"type"`
}

func (b typedBase) GetType() runtime.Type   { return b.Type }
func (b *typedBase) SetType(t runtime.Type) { b.Type = t }

// mustToUnstructured registers from's type in a fresh scheme, stamps it, and converts to a map.
func mustToUnstructured(t *testing.T, from runtime.Typed) (*runtime.Scheme, map[string]any) {
	t.Helper()
	s := runtime.NewScheme()
	s.MustRegister(from, "v1")
	from.SetType(s.MustTypeForPrototype(from))
	u := runtime.NewUnstructured()
	require.NoError(t, s.Convert(from, &u))
	return s, u.Data
}

// assertMatchesJSON asserts the map serializes identically to encoding/json marshaling from.
func assertMatchesJSON(t *testing.T, from runtime.Typed, data map[string]any) {
	t.Helper()
	std, err := json.Marshal(from)
	require.NoError(t, err)
	ours, err := json.Marshal(data)
	require.NoError(t, err)
	assert.JSONEq(t, string(std), string(ours))
}

// --- Domain suite: shapes mirroring real OCM descriptor objects ---

// ociImageLayer mirrors oci/spec/access/v1.OCIImageLayer; its Size (blob bytes) is why int64
// fidelity matters.
type ociImageLayer struct {
	Type      runtime.Type `json:"type"`
	Reference string       `json:"ref"`
	MediaType string       `json:"mediaType,omitempty"`
	Digest    string       `json:"digest"`
	Size      int64        `json:"size"`
}

type digestSpec struct {
	HashAlgorithm          string `json:"hashAlgorithm"`
	NormalisationAlgorithm string `json:"normalisationAlgorithm"`
	Value                  string `json:"value"`
}

type label struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// resource mirrors a component-descriptor resource: a custom-marshaler field (runtime.Type), a
// TextMarshaler+omitzero field (time.Time), omitempty scalars/slices/maps/pointers, a nested
// struct with an int64, a slice of structs and a map.
type resource struct {
	Type         runtime.Type      `json:"type"`
	Name         string            `json:"name"`
	Version      string            `json:"version"`
	Relation     string            `json:"relation,omitempty"`
	CreationTime time.Time         `json:"creationTime,omitzero"`
	Labels       []label           `json:"labels,omitempty"`
	Digest       *digestSpec       `json:"digest,omitempty"`
	Access       ociImageLayer     `json:"access"`
	Annotations  map[string]string `json:"annotations,omitempty"`
	Size         int64             `json:"size"`
}

func (r *resource) GetType() runtime.Type  { return r.Type }
func (r *resource) SetType(v runtime.Type) { r.Type = v }
func (r *resource) DeepCopyTyped() runtime.Typed {
	c := *r
	c.Labels = append([]label(nil), r.Labels...)
	if r.Digest != nil {
		d := *r.Digest
		c.Digest = &d
	}
	c.Annotations = maps.Clone(r.Annotations)
	return &c
}

// bigSize is a blob size beyond 2^53, i.e. not representable exactly as a float64.
const bigSize = int64(9_007_199_254_740_993)

func fullResource() *resource {
	return &resource{
		Name:         "nginx",
		Version:      "1.27.0",
		Relation:     "external",
		CreationTime: time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC),
		Labels:       []label{{Name: "title", Value: "nginx"}},
		Digest:       &digestSpec{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "jsonNormalisation/v1", Value: "a1b2c3"},
		Access:       ociImageLayer{Type: runtime.NewVersionedType("OCIImageLayer", "v1"), Reference: "ghcr.io/x", Digest: "sha256:a1b2c3", Size: bigSize},
		Annotations:  map[string]string{"env": "prod"},
		Size:         bigSize,
	}
}

func TestResource_RoundTripAndInt64Fidelity(t *testing.T) {
	from := fullResource()
	s, data := mustToUnstructured(t, from)

	// int64 sizes stay concrete int64 (top-level and nested), not lossy float64.
	assert.Equal(t, bigSize, data["size"])
	assert.Equal(t, bigSize, data["access"].(map[string]any)["size"])
	assert.Equal(t, "resource/v1", data["type"])

	back := &resource{}
	require.NoError(t, s.Convert(&runtime.Unstructured{Data: data}, back))
	assert.Equal(t, from, back)
}

func TestResource_OmitBehavior(t *testing.T) {
	t.Run("empty optionals dropped", func(t *testing.T) {
		_, data := mustToUnstructured(t, &resource{Name: "n", Version: "v"})
		for _, k := range []string{"relation", "labels", "digest", "annotations", "creationTime"} {
			assert.NotContains(t, data, k)
		}
		assert.Equal(t, int64(0), data["size"])
		assert.Contains(t, data, "access")
	})

	t.Run("populated optionals present, matches encoding/json", func(t *testing.T) {
		from := fullResource()
		_, data := mustToUnstructured(t, from)
		assert.Equal(t, "external", data["relation"])
		assert.Equal(t, "2026-07-01T12:00:00Z", data["creationTime"]) // time.Time via TextMarshaler
		assertMatchesJSON(t, from, data)
	})
}

// descResource mirrors the descriptor pattern more closely: inline-embedded metas and labels
// whose Value is arbitrary json.RawMessage.
type descLabel struct {
	Name  string          `json:"name"`
	Value json.RawMessage `json:"value"`
}

type descObjectMeta struct {
	Name  string            `json:"name"`
	Extra map[string]string `json:"extraIdentity,omitempty"`
}

type descElementMeta struct {
	descObjectMeta `json:",inline"`
	Labels         []descLabel `json:"labels,omitempty"`
}

type descResource struct {
	Type            runtime.Type `json:"type"`
	descElementMeta `json:",inline"`
	Size            int64 `json:"size"`
}

func (r *descResource) GetType() runtime.Type  { return r.Type }
func (r *descResource) SetType(v runtime.Type) { r.Type = v }
func (r *descResource) DeepCopyTyped() runtime.Typed {
	c := *r
	c.Labels = append([]descLabel(nil), r.Labels...)
	return &c
}

func TestDescResource_InlineAndRawMessage(t *testing.T) {
	from := &descResource{
		descElementMeta: descElementMeta{
			descObjectMeta: descObjectMeta{Name: "nginx", Extra: map[string]string{"arch": "amd64"}},
			Labels: []descLabel{
				{Name: "sizes", Value: json.RawMessage(`{"a":9007199254740993}`)},
				{Name: "scalar", Value: json.RawMessage(`"passed"`)},
			},
		},
		Size: bigSize,
	}
	_, data := mustToUnstructured(t, from)

	// Inline meta fields are promoted to the top level, not nested.
	assert.Equal(t, "nginx", data["name"])
	assert.Equal(t, map[string]any{"arch": "amd64"}, data["extraIdentity"])
	assert.NotContains(t, data, "descObjectMeta")

	// A big integer inside a json.RawMessage survives (as a lossless json.Number).
	sizes := data["labels"].([]any)[0].(map[string]any)["value"].(map[string]any)
	assert.Equal(t, json.Number("9007199254740993"), sizes["a"])

	assertMatchesJSON(t, from, data)
}

// --- Marshalers ---

type valueMarshaler struct{ A, B string }

func (m valueMarshaler) MarshalJSON() ([]byte, error) {
	return []byte(`"` + m.A + "-" + m.B + `"`), nil
}

type ptrMarshaler struct{ V string }

func (m *ptrMarshaler) MarshalJSON() ([]byte, error) { return []byte(`"ptr:` + m.V + `"`), nil }

// shadowTime embeds time.Time (value-receiver MarshalJSON) but shadows it with a pointer-receiver
// one, like descriptor/v2.Time. encoding/json (and the converter) must use the pointer marshaler.
type shadowTime struct{ time.Time }

func (t *shadowTime) MarshalJSON() ([]byte, error) {
	if t.Time.IsZero() {
		return []byte("null"), nil
	}
	return []byte(`"` + t.Time.UTC().Format(time.RFC3339) + `"`), nil
}

type marshalerHost struct {
	typedBase
	Value  valueMarshaler            `json:"value"`
	InMap  map[string]valueMarshaler `json:"inMap"`
	InSlic []valueMarshaler          `json:"inSlice"`
	Ptr    *ptrMarshaler             `json:"ptr"`
	Shadow shadowTime                `json:"shadow"`
}

func (h *marshalerHost) DeepCopyTyped() runtime.Typed { c := *h; return &c }

func TestMarshalers(t *testing.T) {
	from := &marshalerHost{
		Value:  valueMarshaler{A: "x", B: "y"},
		InMap:  map[string]valueMarshaler{"k": {A: "m", B: "n"}},
		InSlic: []valueMarshaler{{A: "s", B: "t"}},
		Ptr:    &ptrMarshaler{V: "z"},
		Shadow: shadowTime{time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)},
	}
	_, data := mustToUnstructured(t, from)

	assert.Equal(t, "marshalerHost/v1", data["type"])          // runtime.Type -> string
	assert.Equal(t, "x-y", data["value"])                      // value receiver collapses to scalar
	assert.Equal(t, map[string]any{"k": "m-n"}, data["inMap"]) // ...inside a map
	assert.Equal(t, []any{"s-t"}, data["inSlice"])             // ...and a slice
	assert.Equal(t, "ptr:z", data["ptr"])                      // pointer receiver
	assert.Equal(t, "2026-07-01T12:00:00Z", data["shadow"])    // shadowing pointer marshaler wins
	assertMatchesJSON(t, from, data)
}

func TestMarshalers_ShadowZeroIsNull(t *testing.T) {
	from := &marshalerHost{Ptr: &ptrMarshaler{}}
	_, data := mustToUnstructured(t, from)
	assert.Nil(t, data["shadow"]) // pointer marshaler encodes zero time as null
	assertMatchesJSON(t, from, data)
}

// --- Numbers ---

type numHost struct {
	typedBase
	I   int         `json:"i"`
	I64 int64       `json:"i64"`
	U   uint        `json:"u"`
	U64 uint64      `json:"u64"`
	F32 float32     `json:"f32"`
	F64 float64     `json:"f64"`
	Num json.Number `json:"num"`
}

func (h *numHost) DeepCopyTyped() runtime.Typed { c := *h; return &c }

func TestNumbers(t *testing.T) {
	t.Run("kinds and fidelity", func(t *testing.T) {
		from := &numHost{I: -1, I64: bigSize, U: 1, F32: 0.1, F64: 3.5, Num: "9007199254740993"}
		s, data := mustToUnstructured(t, from)

		for _, k := range []string{"i", "i64", "u"} {
			assert.IsTypef(t, int64(0), data[k], "%s should be int64", k)
		}
		assert.Equal(t, bigSize, data["i64"])        // >2^53 stays exact
		assert.Equal(t, 0.1, data["f32"])            // 32-bit shortest, not widened
		assert.Equal(t, int64(bigSize), data["num"]) // json.Number field -> concrete int64
		assertMatchesJSON(t, from, data)

		back := &numHost{}
		require.NoError(t, s.Convert(&runtime.Unstructured{Data: data}, back))
		assert.Equal(t, from, back)
	})

	t.Run("uint64 above MaxInt64 stays lossless", func(t *testing.T) {
		const huge = uint64(1) << 63
		from := &numHost{U64: huge}
		s, data := mustToUnstructured(t, from)
		assert.Equal(t, json.Number("9223372036854775808"), data["u64"])
		assertMatchesJSON(t, from, data)

		back := &numHost{}
		require.NoError(t, s.Convert(&runtime.Unstructured{Data: data}, back))
		assert.Equal(t, huge, back.U64)
	})

	t.Run("NaN and Inf are rejected", func(t *testing.T) {
		s := runtime.NewScheme()
		s.MustRegister(&numHost{}, "v1")
		typ := s.MustTypeForPrototype(&numHost{})
		for _, f := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
			u := runtime.NewUnstructured()
			err := s.Convert(&numHost{typedBase: typedBase{Type: typ}, F64: f}, &u)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "NaN and Inf")
		}
	})
}

// --- JSON tag semantics ---

type tagHost struct {
	typedBase
	Kept     string `json:"kept"`
	Omitted  string `json:"omitted,omitempty"`
	Ignored  string `json:"-"`
	Bytes    []byte `json:"bytes"`
	Untagged string
	Quoted   int64 `json:"quoted,string"`
}

func (h *tagHost) DeepCopyTyped() runtime.Typed { c := *h; return &c }

func TestJSONTags(t *testing.T) {
	from := &tagHost{Kept: "yes", Ignored: "secret", Bytes: []byte("hi"), Untagged: "u", Quoted: 42}
	_, data := mustToUnstructured(t, from)

	assert.Equal(t, "yes", data["kept"])
	assert.NotContains(t, data, "omitted") // omitempty, empty
	assert.NotContains(t, data, "Ignored") // json:"-"
	assert.Equal(t, "aGk=", data["bytes"]) // []byte -> base64
	assert.Equal(t, "u", data["Untagged"]) // keyed by Go field name
	assert.Equal(t, "42", data["quoted"])  // ,string -> quoted scalar
	assertMatchesJSON(t, from, data)
}

// --- Embedding ---

type base struct {
	Name string `json:"name"`
	Only string `json:"only"`
}

type marker interface{ isMarker() }

type markerImpl struct {
	X string `json:"x"`
}

func (markerImpl) isMarker() {}

type embedHost struct {
	typedBase
	base   `json:",inline"` // inlined
	marker                  // unexported embedded interface -> ignored
	Ptr    *base            `json:"ptr,omitempty"`
	Name   string           `json:"name"` // shadows base.Name
}

func (h *embedHost) DeepCopyTyped() runtime.Typed { c := *h; return &c }

func TestEmbedding(t *testing.T) {
	from := &embedHost{base: base{Name: "INNER", Only: "base"}, marker: markerImpl{X: "hidden"}, Name: "OUTER"}
	_, data := mustToUnstructured(t, from)

	assert.Equal(t, "OUTER", data["name"]) // shallow field wins over embedded
	assert.Equal(t, "base", data["only"])  // non-colliding embedded field promoted
	assert.NotContains(t, data, "x")       // embedded interface not flattened
	assert.NotContains(t, data, "ptr")     // omitempty nil pointer
	assertMatchesJSON(t, from, data)

	from.Ptr = &base{Name: "p"}
	_, data = mustToUnstructured(t, from)
	assert.Equal(t, "p", data["ptr"].(map[string]any)["name"]) // non-nil embedded pointer inlined
}

// --- Map keys ---

type textKey struct{ s string }

func (k textKey) MarshalText() ([]byte, error) { return []byte("K:" + k.s), nil }

type mapKeyHost struct {
	typedBase
	IntKeys map[int]string     `json:"intKeys"`
	TxtKeys map[textKey]string `json:"txtKeys"`
}

func (h *mapKeyHost) DeepCopyTyped() runtime.Typed { c := *h; return &c }

func TestNonStringMapKeys(t *testing.T) {
	from := &mapKeyHost{IntKeys: map[int]string{1: "a"}, TxtKeys: map[textKey]string{{s: "z"}: "v"}}
	_, data := mustToUnstructured(t, from)
	assert.Equal(t, "a", data["intKeys"].(map[string]any)["1"])
	assert.Equal(t, "v", data["txtKeys"].(map[string]any)["K:z"])
	assertMatchesJSON(t, from, data)
}

// --- Nested interface Typed and Raw fields ---

type innerAccess struct {
	Type      runtime.Type `json:"type"`
	Reference string       `json:"ref"`
	Size      int64        `json:"size"`
}

func (t *innerAccess) GetType() runtime.Type        { return t.Type }
func (t *innerAccess) SetType(v runtime.Type)       { t.Type = v }
func (t *innerAccess) DeepCopyTyped() runtime.Typed { c := *t; return &c }

type nestedHost struct {
	typedBase
	GlobalAccess runtime.Typed `json:"globalAccess,omitempty"`
	RawAccess    *runtime.Raw  `json:"rawAccess,omitempty"`
}

func (h *nestedHost) DeepCopyTyped() runtime.Typed { c := *h; return &c }

func TestNestedTypedAndRaw(t *testing.T) {
	accessType := runtime.NewVersionedType("innerAccess", "v1")
	from := &nestedHost{
		GlobalAccess: &innerAccess{Type: accessType, Reference: "ghcr.io/x", Size: bigSize},
		RawAccess:    &runtime.Raw{Type: accessType, Data: []byte(`{"type":"innerAccess/v1","ref":"y","size":1}`)},
	}
	_, data := mustToUnstructured(t, from)

	// The interface field is walked; nested int64 stays concrete.
	assert.Equal(t, bigSize, data["globalAccess"].(map[string]any)["size"])
	// The nested Raw is expanded via its MarshalJSON, not left as bytes.
	assert.Equal(t, "y", data["rawAccess"].(map[string]any)["ref"])
	assertMatchesJSON(t, from, data)
}

// --- Robustness: reference cycles error instead of overflowing the stack ---

type cyclicHost struct {
	typedBase
	Self *cyclicHost `json:"self,omitempty"`
}

func (h *cyclicHost) DeepCopyTyped() runtime.Typed { c := *h; return &c }

func TestCycleErrors(t *testing.T) {
	s := runtime.NewScheme()
	s.MustRegister(&cyclicHost{}, "v1")
	typ := s.MustTypeForPrototype(&cyclicHost{})
	from := &cyclicHost{typedBase: typedBase{Type: typ}}
	from.Self = from

	u := runtime.NewUnstructured()
	err := s.Convert(from, &u)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "nesting depth"), "got: %v", err)
}
