package v1

import (
	"fmt"
	"net/url"

	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Type is the Consumer Identity type for any OCI Repository.
// It can be used inside the credential graph as a consumer type and will be
// used when translating from a repository type into a consumer identity.
var Type = runtime.NewUnversionedType("OCIRepository")

func IdentityFromOCIRepository(repository *oci.Repository) (runtime.Identity, error) {
	repoURL, err := runtime.ParseURLAndAllowNoScheme(repository.BaseUrl)
	if err != nil {
		return nil, fmt.Errorf("could not parse OCI repository URL: %w", err)
	}
	if !repoURL.IsAbs() {
		repoURL, err = url.Parse("https://" + repository.BaseUrl)
		if err != nil {
			return nil, fmt.Errorf("could not parse non absolute OCI repository URL %q: %w", repository.BaseUrl, err)
		}
	}

	identity := runtime.Identity{}
	identity.SetType(Type)

	switch port := repoURL.Port(); {
	case port != "":
		identity[runtime.IdentityAttributePort] = port
	case repoURL.Scheme == "https" || repoURL.Scheme == "oci":
		identity[runtime.IdentityAttributePort] = "443"
	case repoURL.Scheme == "http":
		identity[runtime.IdentityAttributePort] = "80"
	}

	if hostname := repoURL.Hostname(); hostname != "" {
		identity[runtime.IdentityAttributeHostname] = hostname
	}
	if path := repoURL.Path; path != "" {
		identity[runtime.IdentityAttributePath] = path
	}
	if scheme := repoURL.Scheme; scheme != "" {
		identity[runtime.IdentityAttributeScheme] = scheme
	}
	return identity, nil
}

func IdentityFromCTFRepository(repository *ctf.Repository) (runtime.Identity, error) {
	identity := runtime.Identity{
		runtime.IdentityAttributePath: repository.FilePath,
	}
	identity.SetType(Type)
	return identity, nil
}
