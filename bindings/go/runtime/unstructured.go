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
	out.Data = DeepCopyJSON(u.Data)
	return out
}
