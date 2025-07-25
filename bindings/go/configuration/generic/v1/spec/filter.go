package spec

import (
	"fmt"
	"slices"

	"ocm.software/open-component-model/bindings/go/runtime"
)

type FilterOptions struct {
	ConfigTypes []runtime.Type
}

// Filter filters the config based on the provided options.
// Only the FilterOptions.ConfigTypes are copied over.
// If none are specified, the config will be empty.
func Filter(config *Config, options *FilterOptions) (*Config, error) {
	filtered := new(Config)
	filtered.Type = config.Type

	for _, entry := range config.Configurations {
		configType := entry.GetType()
		if slices.Contains(options.ConfigTypes, configType) {
			filtered.Configurations = append(filtered.Configurations, entry)
		}
	}

	return filtered, nil
}

// FilterForType filters the configuration for a specific configuration type T
// and returns a slice of typed configurations.
func FilterForType[T runtime.Typed](scheme *runtime.Scheme, config *Config) ([]T, error) {
	typ, err := scheme.TypeForPrototype(*new(T))
	if err != nil {
		return nil, fmt.Errorf("failed to create get type for prototype of type %T: %w", typ, err)
	}

	types := append(scheme.GetTypes()[typ], typ) //nolint:gocritic // appendAssign to new variable should be safe here

	filtered, err := Filter(config, &FilterOptions{
		ConfigTypes: types,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to filter for types %v: %w", types, err)
	}
	typedConfigs := make([]T, 0, len(filtered.Configurations))
	for _, cfg := range filtered.Configurations {
		obj, err := scheme.NewObject(typ)
		if err != nil {
			return nil, fmt.Errorf("failed to create object for type %s: %w", typ, err)
		}
		if err := scheme.Convert(cfg, obj); err != nil {
			return nil, fmt.Errorf("failed to convert config of type %s to object of type %s: %w", cfg.GetType(), typ, err)
		}
		typedConfigs = append(typedConfigs, obj.(T))
	}

	return typedConfigs, nil
}
