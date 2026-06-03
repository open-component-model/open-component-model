package credentialtype_test

// This file defines minimal credential types that mimic the real OCI, Helm, and RSA
// credential packages solely for use in tests. It exists so the plugin module does not
// need to depend on those bindings.

import "ocm.software/open-component-model/bindings/go/runtime"

// ── type names ───────────────────────────────────────────────────────────────

const (
	ociCredentialsType      = "OCICredentials"
	dockerConfigType        = "DockerConfig"
	helmHTTPCredentialsType = "HelmHTTPCredentials"
	rsaCredentialsType      = "RSACredentials"
	credentialsVersion      = "v1"
)

var (
	ociCredentialsVersionedType      = runtime.NewVersionedType(ociCredentialsType, credentialsVersion)
	dockerConfigVersionedType        = runtime.NewVersionedType(dockerConfigType, credentialsVersion)
	helmHTTPCredentialsVersionedType = runtime.NewVersionedType(helmHTTPCredentialsType, credentialsVersion)
	rsaCredentialsVersionedType      = runtime.NewVersionedType(rsaCredentialsType, credentialsVersion)
)

// ── OCI ──────────────────────────────────────────────────────────────────────

type ociCredentials struct {
	Type     runtime.Type `json:"type"`
	Username string       `json:"username,omitempty"`
	Password string       `json:"password,omitempty"`
}

func (c *ociCredentials) GetType() runtime.Type      { return c.Type }
func (c *ociCredentials) SetType(t runtime.Type)     { c.Type = t }
func (c *ociCredentials) DeepCopyTyped() runtime.Typed {
	cp := *c
	return &cp
}

type dockerConfig struct {
	Type             runtime.Type `json:"type"`
	DockerConfigFile string       `json:"dockerConfigFile,omitempty"`
	DockerConfig     string       `json:"dockerConfig,omitempty"`
}

func (c *dockerConfig) GetType() runtime.Type      { return c.Type }
func (c *dockerConfig) SetType(t runtime.Type)     { c.Type = t }
func (c *dockerConfig) DeepCopyTyped() runtime.Typed {
	cp := *c
	return &cp
}

var ociScheme = func() *runtime.Scheme {
	s := runtime.NewScheme()
	s.MustRegisterWithAlias(&dockerConfig{},
		dockerConfigVersionedType,
		runtime.NewUnversionedType(dockerConfigType),
	)
	s.MustRegisterWithAlias(&ociCredentials{},
		ociCredentialsVersionedType,
		runtime.NewUnversionedType(ociCredentialsType),
	)
	return s
}()

// ── Helm ─────────────────────────────────────────────────────────────────────

type helmHTTPCredentials struct {
	Type     runtime.Type `json:"type"`
	Username string       `json:"username,omitempty"`
	Password string       `json:"password,omitempty"`
}

func (c *helmHTTPCredentials) GetType() runtime.Type      { return c.Type }
func (c *helmHTTPCredentials) SetType(t runtime.Type)     { c.Type = t }
func (c *helmHTTPCredentials) DeepCopyTyped() runtime.Typed {
	cp := *c
	return &cp
}

var helmScheme = func() *runtime.Scheme {
	s := runtime.NewScheme()
	s.MustRegisterWithAlias(&helmHTTPCredentials{},
		helmHTTPCredentialsVersionedType,
		runtime.NewUnversionedType(helmHTTPCredentialsType),
	)
	return s
}()

// ── RSA ──────────────────────────────────────────────────────────────────────

type rsaCredentials struct {
	Type          runtime.Type `json:"type"`
	PublicKeyPEM  string       `json:"publicKeyPEM,omitempty"`
	PrivateKeyPEM string       `json:"privateKeyPEM,omitempty"`
}

func (c *rsaCredentials) GetType() runtime.Type      { return c.Type }
func (c *rsaCredentials) SetType(t runtime.Type)     { c.Type = t }
func (c *rsaCredentials) DeepCopyTyped() runtime.Typed {
	cp := *c
	return &cp
}

var rsaScheme = func() *runtime.Scheme {
	s := runtime.NewScheme()
	s.MustRegisterWithAlias(&rsaCredentials{},
		rsaCredentialsVersionedType,
		runtime.NewUnversionedType(rsaCredentialsType),
	)
	return s
}()
