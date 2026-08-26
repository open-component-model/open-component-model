package v1

import "ocm.software/open-component-model/bindings/go/runtime"

// GitHubCredentials represents typed credentials for authenticating against
// GitHub, on github.com as well as on GitHub Enterprise hosts.
//
// The REST API takes a single bearer token whatever its flavour — personal
// access token, OAuth token, App installation token — so a token is the only
// credential this access type can use.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type GitHubCredentials struct {
	// +ocm:jsonschema-gen:enum=GitHubCredentials/v1
	// +ocm:jsonschema-gen:enum:deprecated=GitHubCredentials
	Type runtime.Type `json:"type"`
	// Token is empty for anonymous access, which cannot see private
	// repositories (GitHub answers 404, not 403) and is rate-limited far
	// more aggressively.
	Token string `json:"token,omitempty"`
}

// MustRegisterCredentialType registers GitHubCredentials/v1 (and its
// unversioned alias) in the given scheme.
func MustRegisterCredentialType(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&GitHubCredentials{},
		runtime.NewVersionedType(GitHubCredentialsType, Version),
		runtime.NewUnversionedType(GitHubCredentialsType),
	)
}
