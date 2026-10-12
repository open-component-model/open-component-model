package credentials_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/credentials"
	credentialruntime "ocm.software/open-component-model/bindings/go/credentials/spec/config/runtime"
	v1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type ingestTestCredentials struct {
	Type     runtime.Type `json:"type"`
	Username string       `json:"username,omitempty"`
	Password string       `json:"password,omitempty"`
}

func (c *ingestTestCredentials) GetType() runtime.Type  { return c.Type }
func (c *ingestTestCredentials) SetType(t runtime.Type) { c.Type = t }
func (c *ingestTestCredentials) DeepCopyTyped() runtime.Typed {
	cp := *c
	return &cp
}

func (c *ingestTestCredentials) Validate() error {
	if c.Password != "" && c.Username == "" {
		return errors.New("password is set but username is empty")
	}
	if c.Username == "" {
		return errors.New("no authentication material")
	}
	return nil
}

var ingestTestCredentialsType = runtime.NewVersionedType("IngestTestCredentials", "v1")

type staticCredentialTypeSchemeProvider struct {
	scheme *runtime.Scheme
}

func (p staticCredentialTypeSchemeProvider) GetCredentialTypeScheme() *runtime.Scheme {
	return p.scheme
}

func ingestTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, scheme.RegisterWithAlias(&ingestTestCredentials{}, ingestTestCredentialsType))
	return scheme
}

func consumerWithCredentials(creds ...runtime.Typed) credentialruntime.Consumer {
	return credentialruntime.Consumer{
		Identities: []runtime.Identity{{
			runtime.IdentityAttributeType: "Wget",
			"hostname":                    "localhost",
			"port":                        "8080",
			"scheme":                      "http",
		}},
		Credentials: creds,
	}
}

func TestIngestTypedCredentialStrictDecoding(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	config := &credentialruntime.Config{
		Consumers: []credentialruntime.Consumer{
			consumerWithCredentials(&runtime.Raw{
				Type: ingestTestCredentialsType,
				Data: []byte(`{"type":"IngestTestCredentials/v1","properties":{"username":"alice","password":"secret"}}`),
			}),
		},
	}

	_, err := credentials.ToGraph(ctx, config, credentials.Options{
		CredentialTypeSchemeProvider: staticCredentialTypeSchemeProvider{scheme: ingestTestScheme(t)},
	})
	r.Error(err)
	r.Contains(err.Error(), `unknown field "properties"`)
	r.Contains(err.Error(), ingestTestCredentialsType.String())
	r.Contains(err.Error(), "localhost", "error should identify the consumer identity")
}

func TestIngestTypedCredentialValidation(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	config := &credentialruntime.Config{
		Consumers: []credentialruntime.Consumer{
			consumerWithCredentials(&runtime.Raw{
				Type: ingestTestCredentialsType,
				Data: []byte(`{"type":"IngestTestCredentials/v1","password":"secret"}`),
			}),
		},
	}

	_, err := credentials.ToGraph(ctx, config, credentials.Options{
		CredentialTypeSchemeProvider: staticCredentialTypeSchemeProvider{scheme: ingestTestScheme(t)},
	})
	r.Error(err)
	r.Contains(err.Error(), "password is set but username is empty")
	r.Contains(err.Error(), ingestTestCredentialsType.String())
}

func TestIngestTypedCredentialWellFormed(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	identity := runtime.Identity{
		runtime.IdentityAttributeType: "Wget",
		"hostname":                    "localhost",
		"port":                        "8080",
		"scheme":                      "http",
	}
	config := &credentialruntime.Config{
		Consumers: []credentialruntime.Consumer{
			consumerWithCredentials(&runtime.Raw{
				Type: ingestTestCredentialsType,
				Data: []byte(`{"type":"IngestTestCredentials/v1","username":"alice","password":"secret"}`),
			}),
		},
	}

	graph, err := credentials.ToGraph(ctx, config, credentials.Options{
		CredentialTypeSchemeProvider: staticCredentialTypeSchemeProvider{scheme: ingestTestScheme(t)},
	})
	r.NoError(err)

	resolved, err := graph.Resolve(ctx, identity)
	r.NoError(err)
	creds, ok := resolved.(*ingestTestCredentials)
	r.True(ok, "expected *ingestTestCredentials, got %T", resolved)
	r.Equal("alice", creds.Username)
	r.Equal("secret", creds.Password)
}

func TestIngestDirectCredentialsWithArbitraryProperties(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	identity := runtime.Identity{
		runtime.IdentityAttributeType: "Wget",
		"hostname":                    "localhost",
		"port":                        "8080",
		"scheme":                      "http",
	}
	config := &credentialruntime.Config{
		Consumers: []credentialruntime.Consumer{
			consumerWithCredentials(&runtime.Raw{
				Type: runtime.NewVersionedType(v1.CredentialsType, v1.Version),
				Data: []byte(`{"type":"Credentials/v1","properties":{"username":"alice","password":"secret","anything-goes":"yes"}}`),
			}),
		},
	}

	graph, err := credentials.ToGraph(ctx, config, credentials.Options{
		CredentialTypeSchemeProvider: staticCredentialTypeSchemeProvider{scheme: ingestTestScheme(t)},
	})
	r.NoError(err)

	resolved, err := graph.Resolve(ctx, identity)
	r.NoError(err)
	creds, ok := resolved.(*v1.DirectCredentials)
	r.True(ok, "expected *v1.DirectCredentials, got %T", resolved)
	r.Equal("alice", creds.Properties["username"])
	r.Equal("yes", creds.Properties["anything-goes"])
}

// exampleConsumerIdentity is a local consumer-identity type used to exercise alias
// canonicalization in the credential graph without importing a concrete identity
// from another binding (disallowed by the layering rules, see golangci.yml
// depguard). Its canonical+alias shape mirrors a real identity type.
type exampleConsumerIdentity struct {
	Type runtime.Type `json:"type"`
}

func (e *exampleConsumerIdentity) GetType() runtime.Type        { return e.Type }
func (e *exampleConsumerIdentity) SetType(t runtime.Type)       { e.Type = t }
func (e *exampleConsumerIdentity) DeepCopyTyped() runtime.Typed { cp := *e; return &cp }

func TestIngestConsumerIdentityAliasCanonicalization(t *testing.T) {
	ctx := t.Context()

	identityScheme := runtime.NewScheme()
	identityScheme.MustRegisterWithAlias(&exampleConsumerIdentity{},
		runtime.NewVersionedType("Example", "v1"),
		runtime.NewUnversionedType("Example"),
		runtime.NewVersionedType("Alias", "v1"),
		runtime.NewUnversionedType("Alias"),
		runtime.NewVersionedType("alias", "v1"),
		runtime.NewUnversionedType("alias"),
	)

	for _, tc := range []struct {
		name         string
		consumerType string
	}{
		{"canonical consumer matches", "Example"},
		{"Alias matches", "Alias"},
		{"lowercase alias matches", "alias"},
		{"versioned Alias matches", "Alias/v1"},
		{"versioned consumer matches", "Example/v1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)

			config := &credentialruntime.Config{
				Consumers: []credentialruntime.Consumer{
					{
						Identities: []runtime.Identity{{
							runtime.IdentityAttributeType: tc.consumerType,
							"hostname":                    "localhost",
							"port":                        "8080",
							"scheme":                      "http",
						}},
						Credentials: []runtime.Typed{&v1.DirectCredentials{
							Type:       runtime.NewVersionedType(v1.CredentialsType, v1.Version),
							Properties: map[string]string{"username": "alice"},
						}},
					},
				},
			}

			graph, err := credentials.ToGraph(ctx, config, credentials.Options{
				ConsumerIdentityTypeScheme: identityScheme,
			})
			r.NoError(err)

			resolved, err := graph.Resolve(ctx, runtime.Identity{
				runtime.IdentityAttributeType: "Example",
				"hostname":                    "localhost",
				"port":                        "8080",
				"scheme":                      "http",
			})
			r.NoError(err)
			dc, ok := resolved.(*v1.DirectCredentials)
			r.True(ok, "expected *v1.DirectCredentials, got %T", resolved)
			r.Equal("alice", dc.Properties["username"])
		})
	}

	t.Run("without scheme an alias entry is not canonicalized", func(t *testing.T) {
		r := require.New(t)

		config := &credentialruntime.Config{
			Consumers: []credentialruntime.Consumer{
				{
					Identities: []runtime.Identity{{
						runtime.IdentityAttributeType: "Alias",
						"hostname":                    "localhost",
					}},
					Credentials: []runtime.Typed{&v1.DirectCredentials{
						Type:       runtime.NewVersionedType(v1.CredentialsType, v1.Version),
						Properties: map[string]string{"username": "alice"},
					}},
				},
			},
		}

		graph, err := credentials.ToGraph(ctx, config, credentials.Options{})
		r.NoError(err)

		_, err = graph.Resolve(ctx, runtime.Identity{
			runtime.IdentityAttributeType: "Example",
			"hostname":                    "localhost",
		})
		r.ErrorIs(err, credentials.ErrNotFound)
	})
}
