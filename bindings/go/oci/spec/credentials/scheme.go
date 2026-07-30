package credentials

import (
	"ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// CredentialRepositoryConfigType is the canonical versioned type for the
// DockerConfig credential repository configuration.
var CredentialRepositoryConfigType = runtime.NewVersionedType("DockerConfig", "v1")

// Scheme contains the credential repository configuration types provided by
// this package (currently DockerConfig).
//
// It does NOT contain credential payload types such as OCICredentials.
// Credential payloads describe authentication material consumed by repositories,
// while a credential-repository-config scheme describes how to LOCATE
// credentials and is used by the credential graph to map specs to repository
// plugins. Conflating the two would cause OCICredentials specs to be wired to
// the OCICredentialRepository plugin, which is incorrect.
//
// To register OCICredentials as a known credential type with the credential
// graph, use v1.MustRegisterCredentialType or the pre-built CredentialTypeScheme.
var Scheme = runtime.NewScheme()

// CredentialTypeScheme contains credential payload types that this package
// owns (currently OCICredentials/v1). It is intended to be passed to
// CredentialRepositoryRegistry.Register so the credential graph can
// deserialize typed credentials it finds in configuration.
var CredentialTypeScheme = runtime.NewScheme()

func init() {
	MustAddToScheme(Scheme)
	v1.MustRegisterCredentialType(CredentialTypeScheme)
}

// MustAddToScheme registers credential repository configuration types provided
// by this package (DockerConfig) into the given scheme.
//
// This MUST NOT be used to register credential payload types such as
// OCICredentials — see Scheme docs for the rationale.
func MustAddToScheme(scheme *runtime.Scheme) {
	dockerConfig := &v1.DockerConfig{}
	scheme.MustRegisterWithAlias(dockerConfig,
		CredentialRepositoryConfigType,
		runtime.NewUnversionedType(v1.DockerConfigType),
	)
}
