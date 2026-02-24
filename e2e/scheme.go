package e2e

import (
	"ocm.software/open-component-model/bindings/go/configuration"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var DefaultScheme = runtime.NewScheme()

func init() {
	if err := Register(DefaultScheme); err != nil {
		panic(err)
	}
}

func Register(scheme *runtime.Scheme) error {
	// Register the generic config scheme so we can parse the outer envelope
	if err := scheme.RegisterScheme(configuration.Scheme); err != nil {
		return err
	}

	// Register our E2E config wrappers
	scheme.MustRegisterWithAlias(
		&RegistryProviderConfig{},
		runtime.NewVersionedType(RegistryProviderConfigType, RegistryProviderConfigTypeV1),
		runtime.NewUnversionedType(RegistryProviderConfigType),
	)

	scheme.MustRegisterWithAlias(
		&ClusterProviderConfig{},
		runtime.NewVersionedType(ClusterProviderConfigType, ClusterProviderConfigTypeV1),
		runtime.NewUnversionedType(ClusterProviderConfigType),
	)

	scheme.MustRegisterWithAlias(
		&CLIProviderConfig{},
		runtime.NewVersionedType(CLIProviderConfigType, CLIProviderConfigTypeV1),
		runtime.NewUnversionedType(CLIProviderConfigType),
	)

	// Register our specific provider implementations
	scheme.MustRegisterWithAlias(
		&ZotProviderSpec{},
		runtime.NewVersionedType(ZotProviderType, ZotProviderTypeV1),
		runtime.NewUnversionedType(ZotProviderType),
	)

	scheme.MustRegisterWithAlias(
		&KindProviderSpec{},
		runtime.NewVersionedType(KindProviderType, KindProviderTypeV1),
		runtime.NewUnversionedType(KindProviderType),
	)

	scheme.MustRegisterWithAlias(
		&ImageCLIProviderSpec{},
		runtime.NewVersionedType(ImageCLIProviderType, ImageCLIProviderTypeV1),
		runtime.NewUnversionedType(ImageCLIProviderType),
	)

	scheme.MustRegisterWithAlias(
		&BinaryCLIProviderSpec{},
		runtime.NewVersionedType(BinaryCLIProviderType, BinaryCLIProviderTypeV1),
		runtime.NewUnversionedType(BinaryCLIProviderType),
	)

	return nil
}
