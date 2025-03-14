package runtime

import (
	"encoding/json"
)

type Unstructured struct {
	Data map[string]any `json:"-"`
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

func (u Unstructured) SetType(v Type) {
	u.Data["type"] = v
}

func (u Unstructured) GetType() Type {
	v, _ := Get[Type](u, "type")
	return v
}

func Get[T any](u Unstructured, key string) (T, bool) {
	v, ok := u.Data[key]
	if !ok {
		return *new(T), false
	}
	t, ok := v.(T)
	return t, ok
}

func (u Unstructured) MarshalJSON() ([]byte, error) {
	return json.Marshal(u.Data)
}

func (u Unstructured) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &u.Data)
}
