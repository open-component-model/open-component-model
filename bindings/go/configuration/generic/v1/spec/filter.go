package spec

import (
	"fmt"
	"slices"

	"ocm.software/open-component-model/bindings/go/runtime"
)

type FilterOptions struct {
	ConfigTypes []runtime.Type
}

// FilterWithRemainder filters the config based on the provided options.
// It returns two configs: filtered contains entries whose type is in
// FilterOptions.ConfigTypes; remainder contains all other entries.
// If no ConfigTypes are specified, all entries are placed in remainder.
func FilterWithRemainder(config *Config, options *FilterOptions) (*Config, *Config, error) {
	if config == nil {
		return nil, nil, fmt.Errorf("config must not be nil")
	}
	if options == nil {
		return nil, nil, fmt.Errorf("options must not be nil")
	}
	filtered := &Config{Type: config.Type}
	remainder := &Config{Type: config.Type}
	for _, entry := range config.Configurations {
		if slices.Contains(options.ConfigTypes, entry.GetType()) {
			filtered.Configurations = append(filtered.Configurations, entry)
		} else {
			remainder.Configurations = append(remainder.Configurations, entry)
		}
	}
	return filtered, remainder, nil
}

// Filter filters the config based on the provided options.
// Only the FilterOptions.ConfigTypes are copied over.
// If none are specified, the config will be empty.
func Filter(config *Config, options *FilterOptions) (*Config, error) {
	filtered, _, err := FilterWithRemainder(config, options)
	return filtered, err
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
