// Package internal holds non-exported helpers shared across the maven binding.
package internal

import (
	"fmt"

	mavenaccess "ocm.software/open-component-model/bindings/go/maven/spec/access"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// CredentialConsumerIdentity builds the credential consumer identity for a Maven
// repository URL. The identity type is the unversioned "MavenRepository". Returns
// an error when repoURL is empty.
func CredentialConsumerIdentity(repoURL string) (runtime.Identity, error) {
	if repoURL == "" {
		return nil, fmt.Errorf("no maven repository specified")
	}
	identity, err := runtime.ParseURLToIdentity(repoURL)
	if err != nil {
		return nil, fmt.Errorf("error parsing maven repository URL to identity: %w", err)
	}
	identity.SetType(runtime.NewUnversionedType(mavenaccess.MavenRepositoryConsumerType))
	return identity, nil
}
