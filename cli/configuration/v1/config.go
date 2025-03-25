package v1

import (
	"encoding/json"

	"ocm.software/open-component-model/bindings/go/runtime"
)

var scheme = runtime.NewScheme()

func init() {
	scheme.MustRegisterWithAlias(&Config{}, runtime.NewUngroupedVersionedType(ConfigType, ConfigTypeV1))
}

const (
	ConfigType   = "generic.config.ocm.software"
	ConfigTypeV1 = "v1"
)

// Config holds configuration entities loaded through a configuration file.
type Config struct {
	runtime.Type   `json:"type"`
	Configurations []Configuration `json:"configurations,omitempty"`
}

// Configuration holds a single configuration entity.
type Configuration struct {
	runtime.Raw `json:",inline"`
}

// UnmarshalJSON takes raw data and unmarshalls it into a Configuration object.
func (a *Configuration) UnmarshalJSON(data []byte) error {
	raw := &runtime.Raw{}
	if err := json.Unmarshal(data, raw); err != nil {
		return err
	}
	a.Raw = *raw
	return nil
}

// MarshalJSON creates a JSON representation of the given object's Raw data.
func (a *Configuration) MarshalJSON() ([]byte, error) {
	return json.Marshal(a.Raw)
}
