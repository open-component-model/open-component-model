package credentials_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"ocm.software/open-component-model/bindings/go/oci/spec/credentials"
	credentialsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// TestSchemeContainsOnlyRepositoryConfigTypes verifies that the package-level
// Scheme exposes the credential repository configuration types
// (DockerConfig) and does NOT contain credential payload types
// (OCICredentials).
//
// Mixing the two would cause OCICredentials specs to be wired to the
// OCICredentialRepository plugin via
// RegisterInternalCredentialRepositoryPlugin, which iterates every type in
// the scheme returned by GetCredentialRepositoryScheme.
func TestSchemeContainsOnlyRepositoryConfigTypes(t *testing.T) {
	dockerConfigType := runtime.NewVersionedType(credentialsv1.DockerConfigType, credentialsv1.Version)
	ociCredentialsType := runtime.NewVersionedType(credentialsv1.OCICredentialsType, credentialsv1.Version)

	assert.True(t, credentials.Scheme.IsRegistered(dockerConfigType),
		"DockerConfig must be registered in the credential repository scheme")
	assert.True(t, credentials.Scheme.IsRegistered(runtime.NewUnversionedType(credentialsv1.DockerConfigType)),
		"unversioned DockerConfig must be registered in the credential repository scheme")

	assert.False(t, credentials.Scheme.IsRegistered(ociCredentialsType),
		"OCICredentials is a credential payload, not a repository config, and must not be in Scheme")
	assert.False(t, credentials.Scheme.IsRegistered(runtime.NewUnversionedType(credentialsv1.OCICredentialsType)),
		"unversioned OCICredentials must not be in Scheme")
}

// TestCredentialTypeSchemeContainsOnlyPayloadTypes verifies that
// CredentialTypeScheme exposes credential payload types (OCICredentials)
// and does NOT contain credential repository configuration types
// (DockerConfig). It is the scheme that callers should pass to
// CredentialRepositoryRegistry.Register to teach the credential graph about
// typed OCI credentials.
func TestCredentialTypeSchemeContainsOnlyPayloadTypes(t *testing.T) {
	dockerConfigType := runtime.NewVersionedType(credentialsv1.DockerConfigType, credentialsv1.Version)
	ociCredentialsType := runtime.NewVersionedType(credentialsv1.OCICredentialsType, credentialsv1.Version)

	assert.True(t, credentials.CredentialTypeScheme.IsRegistered(ociCredentialsType),
		"OCICredentials must be registered in the credential type scheme")
	assert.True(t, credentials.CredentialTypeScheme.IsRegistered(runtime.NewUnversionedType(credentialsv1.OCICredentialsType)),
		"unversioned OCICredentials must be registered in the credential type scheme")

	assert.False(t, credentials.CredentialTypeScheme.IsRegistered(dockerConfigType),
		"DockerConfig is a repository config, not a credential payload, and must not be in CredentialTypeScheme")
	assert.False(t, credentials.CredentialTypeScheme.IsRegistered(runtime.NewUnversionedType(credentialsv1.DockerConfigType)),
		"unversioned DockerConfig must not be in CredentialTypeScheme")
}

// TestMustAddToSchemeRegistersDockerConfigOnly verifies the helper used by
// downstream registrations only adds DockerConfig.
func TestMustAddToSchemeRegistersDockerConfigOnly(t *testing.T) {
	scheme := runtime.NewScheme()
	credentials.MustAddToScheme(scheme)

	assert.True(t, scheme.IsRegistered(runtime.NewVersionedType(credentialsv1.DockerConfigType, credentialsv1.Version)))
	assert.False(t, scheme.IsRegistered(runtime.NewVersionedType(credentialsv1.OCICredentialsType, credentialsv1.Version)),
		"MustAddToScheme must not register OCICredentials")
}

// TestMustRegisterCredentialTypeRegistersOCICredentialsOnly verifies the
// payload-type helper does not leak DockerConfig into the credential type
// scheme.
func TestMustRegisterCredentialTypeRegistersOCICredentialsOnly(t *testing.T) {
	scheme := runtime.NewScheme()
	credentialsv1.MustRegisterCredentialType(scheme)

	assert.True(t, scheme.IsRegistered(runtime.NewVersionedType(credentialsv1.OCICredentialsType, credentialsv1.Version)))
	assert.False(t, scheme.IsRegistered(runtime.NewVersionedType(credentialsv1.DockerConfigType, credentialsv1.Version)),
		"MustRegisterCredentialType must not register DockerConfig")
}
