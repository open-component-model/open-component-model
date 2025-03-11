package runtime

import (
	"encoding/json"
	"fmt"
)

type Raw struct {
	Type `json:"type"`
	Data []byte `json:"-"`
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

	return nil
}
