package runtime

import (
	"errors"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// PluginSpec is the list of plugin capabilities a plugin supports.
// To determine into what type of plugin we have to unmarshal, we unmarshal
// into runtime.Raw first.
// Afterwards, we can use our scheme to convert into the correct runtime.Typed.
// Each runtime.Typed of a plugin knows at what kind of registries it can
// register itself.
type PluginSpec struct {
	CapabilitySpecs      []runtime.Typed
	SupportedConfigTypes []runtime.Type
}

func (spec *PluginSpec) MarshalJSON() ([]byte, error) {
	return nil, errors.New("marshalling runtime plugin spec is unsupported, convert to spec plugin spec")
}

func (spec *PluginSpec) UnmarshalJSON(data []byte) error {
	return errors.New("unmarshalling runtime plugin spec is unsupported, convert from spec plugin spec")
}
