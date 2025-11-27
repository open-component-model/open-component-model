package runtime

import (
	"fmt"

	"ocm.software/open-component-model/bindings/go/plugin/manager/types/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func ConvertToSpec(pluginSpec *PluginSpec) (*spec.PluginSpec, error) {
	scheme := runtime.NewScheme(runtime.WithAllowUnknown())

	plugin := &spec.PluginSpec{
		CapabilitySpecs:      make([]*runtime.Raw, len(pluginSpec.CapabilitySpecs)),
		SupportedConfigTypes: pluginSpec.SupportedConfigTypes,
	}

	for index, capability := range pluginSpec.CapabilitySpecs {
		raw := runtime.Raw{}
		if err := scheme.Convert(capability, &raw); err != nil {
			return nil, fmt.Errorf("error converting capability %s to raw: %w", capability.GetType().String(), err)
		}
		plugin.CapabilitySpecs[index] = &raw
	}
	return plugin, nil
}

func ConvertFromSpec(scheme *runtime.Scheme, pluginSpec *spec.PluginSpec) (*PluginSpec, error) {
	plugin := &PluginSpec{
		CapabilitySpecs:      make([]runtime.Typed, len(pluginSpec.CapabilitySpecs)),
		SupportedConfigTypes: pluginSpec.SupportedConfigTypes,
	}

	for index, raw := range pluginSpec.CapabilitySpecs {
		obj, err := scheme.NewObject(raw.Type)
		if err != nil {
			return nil, fmt.Errorf("error creating new object for type %s: %w", raw.Type.String(), err)
		}
		if err := scheme.Convert(raw, obj); err != nil {
			return nil, fmt.Errorf("error converting raw to type %s: %w", raw.Type.String(), err)
		}
		plugin.CapabilitySpecs[index] = obj
	}
	return plugin, nil
}
