package ocm

import (
	"encoding/json"

	"ocm.software/ocm/api/ocm/compdesc"
)

type Descriptors struct {
	// compdesc.ComponentDescriptor is the internal representation. It should not be marshaled.
	List []*compdesc.ComponentDescriptor
}

func (c Descriptors) MarshalJSON() ([]byte, error) {
	// TODO: Rework the method because its ugly.
	list := `{"components":[`
	for index, desc := range c.List {
		data, err := compdesc.Encode(desc, compdesc.DefaultJSONCodec)
		if err != nil {
			return nil, err
		}
		if index > 0 {
			list += ","
		}
		list += string(data)
	}
	list += `]}`

	return []byte(list), nil
}

func (c *Descriptors) UnmarshalJSON(data []byte) error {
	descriptors := struct {
		List []json.RawMessage `json:"components"`
	}{}
	err := json.Unmarshal(data, &descriptors)
	if err != nil {
		return err
	}

	c.List = make([]*compdesc.ComponentDescriptor, len(descriptors.List))

	var desc *compdesc.ComponentDescriptor
	for i, d := range descriptors.List {
		desc, err = compdesc.Decode(d)
		if err != nil {
			return err
		}
		c.List[i] = desc
	}

	return nil
}
