package v1

import (
	"fmt"
	"strings"

	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// applySubPath sets the path identity attribute from subPath when non-empty,
// overriding any path already extracted from BaseUrl. The leading slash is
// stripped to match the normalization applied by ParseURLToIdentity.
func applySubPath(identity runtime.Identity, subPath string) {
	if subPath != "" {
		identity[runtime.IdentityAttributePath] = strings.TrimPrefix(subPath, "/")
	}
}

func IdentityFromOCIRepository(repository *oci.Repository) (runtime.Identity, error) {
	identity, err := runtime.ParseURLToIdentity(repository.BaseUrl)
	if err != nil {
		return nil, fmt.Errorf("could not parse OCI repository URL: %w", err)
	}
	// When SubPath is explicitly set, it takes precedence over any path
	// component already extracted from BaseUrl.
	// When SubPath is empty, ParseURLToIdentity already extracted any path
	// component from BaseUrl into identity[IdentityAttributePath].
	applySubPath(identity, repository.SubPath)
	identity.SetType(Type)
	return identity, nil
}

func OCIRegistryIdentityFromOCIRepository(repository *oci.Repository) (*OCIRegistryIdentity, error) {
	identity, err := runtime.ParseURLToIdentity(repository.BaseUrl)
	if err != nil {
		return nil, fmt.Errorf("could not parse OCI repository URL: %w", err)
	}
	applySubPath(identity, repository.SubPath)
	return FromIdentity(identity), nil
}

func IdentityFromCTFRepository(repository *ctf.Repository) (runtime.Identity, error) {
	identity := runtime.Identity{
		runtime.IdentityAttributePath: repository.FilePath,
	}
	identity.SetType(Type)
	return identity, nil
}
