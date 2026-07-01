package v1

import (
	"fmt"

	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func IdentityFromOCIRepository(repository *oci.Repository) (runtime.Identity, error) {
	identity, err := runtime.ParseURLToIdentity(repository.BaseUrl)
	if err != nil {
		return nil, fmt.Errorf("could not parse OCI repository URL: %w", err)
	}
	// When SubPath is explicitly set, use it directly (BaseUrl is host-only).
	// When SubPath is empty, ParseURLToIdentity already extracted any path
	// component from BaseUrl into identity[IdentityAttributePath].
	if repository.SubPath != "" {
		identity[runtime.IdentityAttributePath] = repository.SubPath
	}
	identity.SetType(Type)
	return identity, nil
}

func OCIRegistryIdentityFromOCIRepository(repository *oci.Repository) (*OCIRegistryIdentity, error) {
	identity, err := runtime.ParseURLToIdentity(repository.BaseUrl)
	if err != nil {
		return nil, fmt.Errorf("could not parse OCI repository URL: %w", err)
	}
	if repository.SubPath != "" {
		identity[runtime.IdentityAttributePath] = repository.SubPath
	}
	return FromIdentity(identity), nil
}

func IdentityFromCTFRepository(repository *ctf.Repository) (runtime.Identity, error) {
	identity := runtime.Identity{
		runtime.IdentityAttributePath: repository.FilePath,
	}
	identity.SetType(Type)
	return identity, nil
}
