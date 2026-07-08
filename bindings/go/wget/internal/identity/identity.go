package identity

import (
	"fmt"

	"ocm.software/open-component-model/bindings/go/runtime"
	accessspec "ocm.software/open-component-model/bindings/go/wget/spec/access"
)

// url resolves the credential consumer identity for the given
// wget URL. It is shared by the wget access type and the wget input method so that
// credentials configured for a host resolve identically whether the resource is
// declared via an access spec or an input spec.
func CredentialConsumerIdentity(url string) (runtime.Identity, error) {
	if url == "" {
		return nil, fmt.Errorf("url is required")
	}

	identity, err := runtime.ParseURLToIdentity(url)
	if err != nil {
		return nil, fmt.Errorf("error parsing wget URL to identity: %w", err)
	}

	identity.SetType(accessspec.V1VersionedType)

	return identity, nil
}
