package runtime

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
)

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
	d, err := AddTypeIfMissing(u.Data, u.Type)
	if err != nil {
		return nil, fmt.Errorf("could not marshal data into raw: %w", err)
	}
	return d, nil
}

func AddTypeIfMissing(input []byte, typ Type) ([]byte, error) {
	if typ.IsEmpty() {
		return input, nil
	}
	// Use json.Decoder to only scan top-level keys
	// Use map[string]json.RawMessage to avoid full unmarshalling
	var rawMap map[string]json.RawMessage
	if err := json.NewDecoder(bytes.NewReader(input)).Decode(&rawMap); err != nil {
		return nil, err
	}

	if raw, exists := rawMap[IdentityAttributeType]; !exists || bytes.Equal(raw, []byte(`""`)) {
		rawMap[IdentityAttributeType] = json.RawMessage(`"` + typ.String() + `"`)
	}

	// Marshal the modified map back to JSON
	return json.Marshal(rawMap)
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
