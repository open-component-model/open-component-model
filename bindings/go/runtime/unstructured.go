package runtime

import (
	"encoding/json"
	"fmt"
)

// Unstructured is a generic representation of a typed object.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
type Unstructured struct {
	Data map[string]interface{}
}

// UnstructuredFromMixedData creates an Unstructured object from a map[string]any.
// The input data is normalized to ensure that it is in a consistent format for JSON serialization.
// Use this function only when the input data is expected to contain non-JSON-native types that need to be normalized.
// If the input data is already in a JSON-native format, it is more efficient to create an Unstructured object directly
// with the data map, as normalization can be costly.
func UnstructuredFromMixedData(data map[string]any) (*Unstructured, error) {
	normalized, err := normalizeJSONMap(data)
	if err != nil {
		return nil, err
	}
	return &Unstructured{
		Data: normalized,
	}, nil
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
	out.Data = DeepCopyJSON(u.Data)
	return out
}

// isJSONNative recursively checks whether a value consists only of JSON-native types
// (as handled by DeepCopyJSONValue).
func isJSONNative(x any) bool {
	switch v := x.(type) {
	case string, int64, int32, int, bool, float64, float32, nil, json.Number:
		return true
	case map[string]any:
		for _, val := range v {
			if !isJSONNative(val) {
				return false
			}
		}
		return true
	case []any:
		for _, elem := range v {
			if !isJSONNative(elem) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// normalizeJSONValue recursively checks whether a value is a JSON-native type
// (as handled by DeepCopyJSONValue). If it is, the value is returned as-is.
// If not, it is normalized via JSON marshal/unmarshal.
func normalizeJSONValue(x any) (any, error) {
	if isJSONNative(x) {
		return x, nil
	}

	switch v := x.(type) {
	case map[string]any:
		return normalizeJSONMap(v)
	case []any:
		out := make([]any, len(v))
		for i, elem := range v {
			normalized, err := normalizeJSONValue(elem)
			if err != nil {
				return nil, err
			}
			out[i] = normalized
		}
		return out, nil
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("unstructured normalize: cannot marshal: %w", err)
		}
		var normalized any
		if err := json.Unmarshal(data, &normalized); err != nil {
			return nil, fmt.Errorf("unstructured normalize: cannot unmarshal: %w", err)
		}
		return normalized, nil
	}
}

func normalizeJSONMap(m map[string]any) (map[string]any, error) {
	if m == nil {
		return nil, nil
	}
	if isJSONNative(m) {
		return m, nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		normalized, err := normalizeJSONValue(v)
		if err != nil {
			return nil, err
		}
		out[k] = normalized
	}
	return out, nil
}
