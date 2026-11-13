package runtime

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"

	_ "embed"
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
	Data jsontext.Value `json:"-"`
}

func (u *Raw) String() string {
	return string(u.Data)
}

var _ interface {
	json.MarshalerTo
	json.UnmarshalerFrom
	Typed
} = &Raw{}

func (u *Raw) SetType(v Type) {
	u.Type = v
}

func (u *Raw) GetType() Type {
	return u.Type
}

func (u *Raw) MarshalJSONTo(enc *jsontext.Encoder) error {
	return enc.WriteValue(u.Data)
}

func (u *Raw) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	data, err := dec.ReadValue()
	if err != nil {
		return fmt.Errorf("could not read data into raw: %w", err)
	}
	// ReadValue borrows the decoder's buffer, which the next read invalidates.
	data = data.Clone()

	t := &struct {
		Type Type `json:"type"`
	}{}
	if err := json.Unmarshal(data, t, dec.Options()); err != nil {
		return fmt.Errorf("could not unmarshal data into raw: %w", err)
	}
	u.Type = t.Type

	if err := data.Canonicalize(); err != nil {
		return fmt.Errorf("could not canonicalize data: %w", err)
	}
	u.Data = data

	return nil
}

//go:embed schemas/Raw.schema.json
var schemaRaw []byte

func (u Raw) JSONSchema() []byte {
	return schemaRaw
}
