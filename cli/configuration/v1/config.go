package v1

import (
	"encoding/json"

	"ocm.software/open-component-model/bindings/go/runtime"
)

var scheme = runtime.NewScheme()

func init() {
	scheme.MustRegisterWithAlias(&Config{}, runtime.NewVersionedType(ConfigType, ConfigTypeV1))
}

const (
	ConfigType   = "generic.config.ocm.software"
	ConfigTypeV1 = "v1"
)

// Config holds configuration entities loaded through a configuration file.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type Config struct {
	Type           runtime.Type    `json:"type"`
	Configurations []Configuration `json:"configurations"`
}

// Configuration holds a single configuration entity.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type Configuration struct {
	*runtime.Raw `json:",inline"`
}

// UnmarshalJSON takes raw data and unmarshalls it into a Configuration object.
func (a *Configuration) UnmarshalJSON(data []byte) error {
	raw := &runtime.Raw{}
	if err := json.Unmarshal(data, raw); err != nil {
		return err
	}
	a.Raw = raw
	return nil
}

// MarshalJSON creates a JSON representation of the given object's Raw data.
func (a *Configuration) MarshalJSON() ([]byte, error) {
	return json.Marshal(a.Raw)
}
