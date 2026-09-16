// Package internal holds non-exported helpers shared across the pypi binding.
package internal

import (
	"fmt"

	pypiaccess "ocm.software/open-component-model/bindings/go/pypi/spec/access"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// CredentialConsumerIdentity builds the credential consumer identity for a PyPI
// index URL. The identity type is the unversioned "PyPIRepository". Returns an
// error when indexURL is empty.
func CredentialConsumerIdentity(indexURL string) (runtime.Identity, error) {
	if indexURL == "" {
		return nil, fmt.Errorf("no pypi index specified")
	}
	identity, err := runtime.ParseURLToIdentity(indexURL)
	if err != nil {
		return nil, fmt.Errorf("error parsing pypi index URL to identity: %w", err)
	}
	identity.SetType(runtime.NewUnversionedType(pypiaccess.PyPIRepositoryConsumerType))
	return identity, nil
}
