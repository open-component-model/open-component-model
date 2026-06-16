package v1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

// OCICredentialsType is the type name for OCI registry credentials.
const OCICredentialsType = "OCICredentials"

var OCICredentialsVersionedType = runtime.NewVersionedType(OCICredentialsType, Version)

// MustRegisterCredentialType registers OCICredentials/v1 (and its unversioned
// alias) in the given scheme.
//
// OCICredentials is a credential payload type (it carries username/password
// or tokens that authenticate against an OCI registry). It is NOT a credential
// repository configuration — it must be registered with the credential-type
// scheme of the credential graph (see
// credentialrepository.RepositoryRegistry.Register), never with a scheme that
// maps repository specs to repository plugins.
func MustRegisterCredentialType(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&OCICredentials{},
		OCICredentialsVersionedType,
		runtime.NewUnversionedType(OCICredentialsType),
	)
}

// OCICredentials represents typed credentials for OCI registry authentication.
// It supports username/password and token-based authentication flows used by
// container registries that implement the OCI distribution specification.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type OCICredentials struct {
	// +ocm:jsonschema-gen:enum=OCICredentials/v1
	// +ocm:jsonschema-gen:enum:deprecated=OCICredentials
	Type runtime.Type `json:"type"`
	// Username is the username for basic authentication against the OCI registry.
	// Used together with Password. Mutually exclusive with token-based authentication
	// (AccessToken or RefreshToken); token fields take precedence when present.
	Username string `json:"username,omitempty"`
	// Password is the password for basic authentication against the OCI registry.
	// Used together with Username.
	Password string `json:"password,omitempty"`
	// AccessToken is a bearer token sent directly to the OCI registry (registry token).
	// Used in the Docker token authentication flow after the auth service has issued it.
	// When set, it is forwarded as a Bearer token on registry requests.
	// Reference: https://distribution.github.io/distribution/spec/auth/token/
	AccessToken string `json:"accessToken,omitempty"`
	// RefreshToken is a bearer token sent to the OCI authorization service to obtain
	// an AccessToken (identity token / OAuth2 refresh token).
	// When set, the client exchanges it for a short-lived AccessToken before
	// each registry request.
	// Reference: https://distribution.github.io/distribution/spec/auth/oauth/
	RefreshToken string `json:"refreshToken,omitempty"`
}
