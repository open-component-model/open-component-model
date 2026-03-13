package v1

import (
	"fmt"

	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Type is the Consumer Identity type for any OCI Repository.
// It can be used inside the credential graph as a consumer type and will be
// used when translating from a repository type into a consumer identity.
var Type = runtime.NewUnversionedType("OCIRegistry")

func IdentityFromOCIRepository(repository *oci.Repository) (runtime.Identity, error) {
	identity, err := runtime.ParseURLToIdentity(repository.BaseUrl)
	if err != nil {
		return nil, fmt.Errorf("could not parse OCI repository URL: %w", err)
	}
	identity.SetType(Type)
	return identity, nil
}

func IdentityFromCTFRepository(repository *ctf.Repository) (runtime.Identity, error) {
	identity := runtime.Identity{
		runtime.IdentityAttributePath: repository.FilePath,
	}
	identity.SetType(Type)
	return identity, nil
}
