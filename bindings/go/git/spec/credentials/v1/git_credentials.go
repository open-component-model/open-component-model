package v1

import (
	"fmt"
	"strings"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// GitCredentials supports HTTP basic authentication, bearer tokens, and SSH keys.
// An SSH repository without a private key uses the SSH agent.
//
// Deprecated: For new configurations, use GitHTTPSCredentials,
// GitBearerCredentials, or GitSSHCredentials to select the authentication
// method explicitly.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type GitCredentials struct {
	// +ocm:jsonschema-gen:enum=GitCredentials/v1
	// +ocm:jsonschema-gen:enum:deprecated=GitCredentials/v1,GitCredentials
	Type     runtime.Type `json:"type"`
	Username string       `json:"username,omitempty"`
	// Password is the HTTP password or the SSH key passphrase.
	Password string `json:"password,omitempty"`
	// Token is an HTTP bearer token. Tokens used with Basic authentication belong in Password.
	Token string `json:"token,omitempty"`
	// PrivateKey is a path to an SSH private key file, as in OCM v1.
	// Ignored when PrivateKeyPEM is also set.
	PrivateKey string `json:"privateKey,omitempty"`
	// PrivateKeyPEM is an inline PEM-encoded SSH private key.
	// Takes precedence over PrivateKey when both are set.
	PrivateKeyPEM string `json:"privateKeyPEM,omitempty"`
}

func MustRegisterCredentialType(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&GitCredentials{},
		runtime.NewVersionedType(GitCredentialsType, Version),
		runtime.NewUnversionedType(GitCredentialsType),
	)
}

// GitHTTPSCredentials authenticates Git over HTTPS using HTTP Basic authentication.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type GitHTTPSCredentials struct {
	// +ocm:jsonschema-gen:enum=GitHTTPSCredentials/v1
	Type     runtime.Type `json:"type"`
	// Username is the username required by the Git server.
	Username string       `json:"username"`
	// Password is the server password or access token sent as the HTTP Basic password.
	Password string       `json:"password"`
}

func (c *GitHTTPSCredentials) Validate() error {
	if c.Username == "" {
		return fmt.Errorf("git HTTPS credentials require a username")
	}
	if strings.Contains(c.Username, ":") {
		return fmt.Errorf("git HTTPS username must not contain a colon")
	}
	return nil
}

// GitBearerCredentials authenticates Git over HTTPS using Bearer authentication.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type GitBearerCredentials struct {
	// +ocm:jsonschema-gen:enum=GitBearerCredentials/v1
	Type  runtime.Type `json:"type"`
	// Token is sent in the HTTP Authorization Bearer header.
	Token string       `json:"token"`
}

func (c *GitBearerCredentials) Validate() error {
	if c.Token == "" {
		return fmt.Errorf("HTTP Bearer authentication requires a token")
	}
	return nil
}

// GitSSHCredentials authenticates Git over SSH with a private key or the SSH agent.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type GitSSHCredentials struct {
	// +ocm:jsonschema-gen:enum=GitSSHCredentials/v1
	Type     runtime.Type `json:"type"`
	// Username overrides the SSH URL user. By default, OCM uses the URL user, then git.
	Username string       `json:"username,omitempty"`
	// PrivateKey is the path to an SSH private key file.
	PrivateKey string `json:"privateKey,omitempty"`
	// PrivateKeyPEM is an inline PEM-encoded SSH private key.
	PrivateKeyPEM string `json:"privateKeyPEM,omitempty"`
	// Passphrase decrypts the private key.
	Passphrase string `json:"passphrase,omitempty"`
}

func (c *GitSSHCredentials) Validate() error {
	if c.PrivateKey != "" && c.PrivateKeyPEM != "" {
		return fmt.Errorf("SSH credentials must specify only one of privateKey and privateKeyPEM")
	}
	if c.Passphrase != "" && c.PrivateKey == "" && c.PrivateKeyPEM == "" {
		return fmt.Errorf("SSH passphrase requires a private key")
	}
	return nil
}

// MustRegisterTransportCredentialTypes registers the explicit Git authentication methods.
func MustRegisterTransportCredentialTypes(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&GitHTTPSCredentials{}, runtime.NewVersionedType(GitHTTPSCredentialsType, Version))
	scheme.MustRegisterWithAlias(&GitBearerCredentials{}, runtime.NewVersionedType(GitBearerCredentialsType, Version))
	scheme.MustRegisterWithAlias(&GitSSHCredentials{}, runtime.NewVersionedType(GitSSHCredentialsType, Version))
}
