package v1

import (
	"fmt"

	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	OCIRegistryIdentityType = "OCIRegistry"
	Version                 = "v1"
)

// Type is the unversioned Consumer Identity type for any OCI Repository (backward compat).
var Type = runtime.NewUnversionedType(OCIRegistryIdentityType)

// VersionedType is the versioned consumer identity type.
var VersionedType = runtime.NewVersionedType(OCIRegistryIdentityType, Version)

func IdentityFromOCIRepository(repository *oci.Repository) (runtime.Identity, error) {
	identity, err := runtime.ParseURLToIdentity(repository.BaseUrl)
	if err != nil {
		return nil, fmt.Errorf("could not parse OCI repository URL: %w", err)
	}
	identity.SetType(Type)
	return identity, nil
}

func OCIRegistryIdentityFromOCIRepository(repository *oci.Repository) (*OCIRegistryIdentity, error) {
	identity, err := runtime.ParseURLToIdentity(repository.BaseUrl)
	if err != nil {
		return nil, fmt.Errorf("could not parse OCI repository URL: %w", err)
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
