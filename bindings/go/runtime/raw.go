package runtime

import (
	"encoding/json"
	"fmt"

	_ "embed"

	"github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
)

// Raw is used to hold extensions in external versions.
//
// To use this, make a field which has RawExtension as its type in your external, versioned
// struct, and Typed in your internal struct. You also need to register your
// various plugin types.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
type Raw struct {
	Type `json:"type"`
	Data []byte `json:"-"`
}

func (u *Raw) String() string {
	return string(u.Data)
}

var _ interface {
	json.Marshaler
	json.Unmarshaler
	Typed
} = &Raw{}

func (u *Raw) SetType(v Type) {
	u.Type = v
}

func (u *Raw) GetType() Type {
	return u.Type
}

func (u *Raw) MarshalJSON() ([]byte, error) {
	return u.Data, nil
}

func (u *Raw) UnmarshalJSON(data []byte) error {
	t := &struct {
		Type Type `json:"type"`
	}{}
	err := json.Unmarshal(data, t)
	if err != nil {
		return fmt.Errorf("could not unmarshal data into raw: %w", err)
	}
	u.Type = t.Type
	u.Data = data

	u.Data, err = jsoncanonicalizer.Transform(u.Data)
	if err != nil {
		return fmt.Errorf("could not canonicalize data: %w", err)
	}

	return nil
}

//go:embed schemas/Raw.schema.json
var schemaRaw []byte

func (u Raw) JSONSchema() []byte {
	return schemaRaw
}
