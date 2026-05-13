package v1

import (
	"fmt"
	"path"

	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func IdentityFromOCIRepository(repository *oci.Repository) (runtime.Identity, error) {
	identity, err := runtime.ParseURLToIdentity(repository.BaseUrl)
	if err != nil {
		return nil, fmt.Errorf("could not parse OCI repository URL: %w", err)
	}
	identity.SetType(Type)
	return identity, nil
}

// IdentityFromOCIRepositoryWithSubPath extends [IdentityFromOCIRepository] by
// joining the SubPath into the path attribute, so two repositories on the same
// host with different SubPaths get distinct identities.
func IdentityFromOCIRepositoryWithSubPath(repository *oci.Repository) (runtime.Identity, error) {
	identity, err := IdentityFromOCIRepository(repository)
	if err != nil {
		return nil, err
	}
	if repository.SubPath != "" {
		identity[runtime.IdentityAttributePath] = path.Join(identity[runtime.IdentityAttributePath], repository.SubPath)
	}
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
