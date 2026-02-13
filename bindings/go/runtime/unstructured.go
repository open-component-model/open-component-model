package runtime

import (
	"encoding/json"
)

// Unstructured is a generic representation of a typed object.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
type Unstructured struct {
	Data map[string]interface{}
}

var _ interface {
	json.Marshaler
	json.Unmarshaler
	Typed
} = &Unstructured{}

func NewUnstructured() Unstructured {
	return Unstructured{
		Data: make(map[string]any),
	}
}

func (u *Unstructured) SetType(v Type) {
	u.Data[IdentityAttributeType] = v
}

func (u *Unstructured) GetType() Type {
	v, _ := Get[Type](u, IdentityAttributeType)
	return v
}

func Get[T any](u *Unstructured, key string) (T, bool) {
	v, ok := u.Data[key]
	if !ok {
		return *new(T), false
	}
	t, ok := v.(T)
	return t, ok
}

func (u *Unstructured) MarshalJSON() ([]byte, error) {
	return json.Marshal(u.Data)
}

func (u *Unstructured) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &u.Data)
}

func (u *Unstructured) DeepCopy() *Unstructured {
	if u == nil {
		return nil
	}
	out := new(Unstructured)
	*out = *u

	out.Data = DeepCopyJSON(normalizeJSONMap(u.Data))
	return out
}

// normalizeJSONValue recursively checks whether a value is a JSON-native type
// (as handled by DeepCopyJSONValue). If it is, the value is returned as-is.
// If not, it is normalized via JSON marshal/unmarshal.
func normalizeJSONValue(x interface{}) interface{} {
	switch v := x.(type) {
	case map[string]interface{}:
		return normalizeJSONMap(v)
	case []interface{}:
		if v == nil {
			return v
		}
		out := make([]interface{}, len(v))
		for i, elem := range v {
			out[i] = normalizeJSONValue(elem)
		}
		return out
	case string, int64, bool, float64, nil, json.Number:
		return v
	default:
		data, err := json.Marshal(v)
		if err != nil {
			panic("unstructured normalize: cannot marshal " + err.Error())
		}
		var normalized interface{}
		if err := json.Unmarshal(data, &normalized); err != nil {
			panic("unstructured normalize: cannot unmarshal " + err.Error())
		}
		return normalized
	}
}

func normalizeJSONMap(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = normalizeJSONValue(v)
	}
	return out
}
