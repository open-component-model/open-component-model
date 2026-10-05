package v1

import (
	"fmt"
	"strings"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// GitHTTPSCredentials authenticates Git over HTTPS with a username and password or access token.
// Password can be a password, personal access token, or OAuth access token,
// according to the server's requirements. Credentials are sent using HTTP Basic authentication.
// No provider-specific defaults are applied.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type GitHTTPSCredentials struct {
	// +ocm:jsonschema-gen:enum=GitHTTPSCredentials/v1
	Type     runtime.Type `json:"type"`
	Username string       `json:"username"`
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

// GitBearerCredentials authenticates Git over HTTPS using Authorization: Bearer.
// Use only when the server explicitly requires Bearer authentication. For HTTPS
// cloning with a password or access token, use GitHTTPSCredentials.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type GitBearerCredentials struct {
	// +ocm:jsonschema-gen:enum=GitBearerCredentials/v1
	Type  runtime.Type `json:"type"`
	Token string       `json:"token"`
}

func (c *GitBearerCredentials) Validate() error {
	if c.Token == "" {
		return fmt.Errorf("HTTP Bearer authentication requires a token")
	}
	return nil
}

// GitSSHCredentials authenticates Git over SSH with a private key or the SSH agent.
// Without a key, the SSH agent is used. Username defaults to the URL user, then git.
// Host keys are checked against known_hosts unless the caller supplies a callback.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type GitSSHCredentials struct {
	// +ocm:jsonschema-gen:enum=GitSSHCredentials/v1
	Type     runtime.Type `json:"type"`
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
