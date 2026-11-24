package setup

import (
	"errors"
	"fmt"
	"os"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	ocicachev1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/config/cache/v1alpha1"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

// startupConfigTypes are the configuration types accepted at startup. They configure state shared by
// every namespace, so they MUST NOT come from the per-object spec.ocmConfig.
var startupConfigTypes = []ocmruntime.Type{
	ocmruntime.NewVersionedType(ocicachev1alpha1.ConfigType, ocicachev1alpha1.Version),
	ocmruntime.NewUnversionedType(ocicachev1alpha1.ConfigType),
}

// LoadStartupConfig reads the OCM configuration file passed to the controller with --ocm-config.
// Any type outside [startupConfigTypes] is an error. Anything not allowed will result in a controller startup failure.
func LoadStartupConfig(path string) (_ *genericv1.Config, err error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open ocm config: %w", err)
	}
	defer func() {
		err = errors.Join(err, file.Close())
	}()

	var cfg genericv1.Config
	if err := genericv1.Scheme.Decode(file, &cfg); err != nil {
		return nil, fmt.Errorf("failed to decode ocm config: %w", err)
	}
	supported, unsupported, err := genericv1.FilterWithRemainder(&cfg, &genericv1.FilterOptions{ConfigTypes: startupConfigTypes})
	if err != nil {
		return nil, fmt.Errorf("failed to filter ocm config: %w", err)
	}
	if len(unsupported.Configurations) > 0 {
		types := make([]string, 0, len(unsupported.Configurations))
		for _, entry := range unsupported.Configurations {
			types = append(types, entry.GetType().String())
		}
		return nil, fmt.Errorf("types %v are not supported at startup, supported: %v", types, startupConfigTypes)
	}
	return supported, nil
}
