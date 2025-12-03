package credentials_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/credentials"
	credentialruntime "ocm.software/open-component-model/bindings/go/credentials/spec/config/runtime"
	v1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/dag"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type CredentialPlugin struct {
	ConsumerIdentityTypeAttributes map[runtime.Type]map[string]func(v any) (string, string)
	CredentialFunc                 func(ctx context.Context, identity runtime.Identity, credentials map[string]string) (resolved map[string]string, err error)
}

func (p CredentialPlugin) GetConsumerIdentity(_ context.Context, typed runtime.Typed) (runtime.Identity, error) {
	attrs, ok := p.ConsumerIdentityTypeAttributes[typed.GetType()]
	if !ok {
		return nil, fmt.Errorf("unsupported credential type %v", typed.GetType())
	}

	data, err := json.Marshal(typed)
	if err != nil {
		return nil, err
	}

	mm := make(map[string]interface{})
	if err := json.Unmarshal(data, &mm); err != nil {
		return nil, err
	}

	identity := make(runtime.Identity)
	identity[runtime.IdentityAttributeType] = typed.GetType().String()
	for k, attr := range attrs {
		if val, ok := mm[k]; ok {
			newKey, newVal := attr(val)
			identity[newKey] = newVal
		}
	}

	return identity, nil
}

func (p CredentialPlugin) Resolve(ctx context.Context, identity runtime.Identity, credentials map[string]string) (map[string]string, error) {
	if p.CredentialFunc == nil {
		return nil, fmt.Errorf("no credential function for %v", identity)
	}
	return p.CredentialFunc(ctx, identity, credentials)
}

type RepositoryPlugin struct {
	RepositoryIdentityFunc func(config runtime.Typed) (runtime.Identity, error)
	ResolveFunc            func(ctx context.Context, cfg runtime.Typed, identity runtime.Identity, credentials map[string]string) (map[string]string, error)
}

func (s RepositoryPlugin) GetCredentialRepositoryScheme() *runtime.Scheme {
	return runtime.NewScheme()
}

func (s RepositoryPlugin) ConsumerIdentityForConfig(_ context.Context, config runtime.Typed) (runtime.Identity, error) {
	return s.RepositoryIdentityFunc(config)
}

func (s RepositoryPlugin) Resolve(ctx context.Context, config runtime.Typed, identity runtime.Identity, credentials map[string]string) (map[string]string, error) {
	return s.ResolveFunc(ctx, config, identity, credentials)
}

const testYAML = `
type: credentials.config.ocm.software
repositories:
- repository:
    type: DockerConfig/v1
    dockerConfigFile: "~/.docker/config.json"
    propagateConsumerIdentity: true
- repository:
    type: HashiCorpVault
    serverURL: "https://repository.vault.com/"
consumers:
  - identity:
      type: AWSSecretsManager
      secretId: "vault-access-creds"
    credentials:
      - type: Credentials/v1
        properties:
          roleid: "my-role-id"

  - identity:
      type: HashiCorpVault
      hostname: "myvault.example.com"
    credentials:
      - type: AWSSecretsManager
        secretId: "vault-access-creds"
  - identity:
      type: HashiCorpVault
      hostname: "other.vault.com"
    credentials:
      - type: HashiCorpVault
        serverURL: "https://myvault.example.com/"
        mountPath: "my-engine/my-engine-root"
        path: "my/path/to/my/secret"
        credentialsName: "my-secret-name"

  - identity:
      type: OCIRegistry
      hostname: "docker.io"
    credentials:
      - type: HashiCorpVault
        serverURL: "https://other.vault.com/"
        mountPath: "kv/oci"
        path: "oci/secret/docker"
        credentialsName: "docker-credentials"

  - identity:
      type: HashiCorpVault
      hostname: "repository.vault.com"
    credentials:
      - type: Credentials/v1
        properties:
          role_id: "repository.vault.com-role"
          secret_id: "repository.vault.com-secret"

  - identity:
      type: OCIRegistry
      hostname: "quay.io"
      path: "some-owner/*"
    credentials:
      - type: Credentials/v1
        properties:
          username: some-owner
          password: abc
`

const invalidRecursionYAML = testYAML + `
  - identity:
      type: AWSSecretsManager
      secretId: "recursive-creds"
    credentials:
      - type: AWSSecretsManager
        secretId: "recursive-creds"
`

func GetGraph(t testing.TB, yaml string) (credentials.Resolver, error) {
	t.Helper()
	r := require.New(t)
	scheme := runtime.NewScheme()
	v1.MustRegister(scheme)

	split := strings.Split(yaml, "---")
	var configs []*credentialruntime.Config
	for _, yaml := range split {
		var configv1 v1.Config
		r.NoError(scheme.Decode(strings.NewReader(yaml), &configv1))
		configs = append(configs, credentialruntime.ConvertFromV1(&configv1))
	}

	config := credentialruntime.Merge(configs...)

	getPluginRepositoryFn := func(ctx context.Context, repoType runtime.Typed) (credentials.RepositoryPlugin, error) {
		switch repoType.GetType().String() {
		case "OCIRegistry":
			return RepositoryPlugin{
				RepositoryIdentityFunc: func(config runtime.Typed) (runtime.Identity, error) {
					var mm map[string]interface{}
					if err := json.Unmarshal(config.(*runtime.Raw).Data, &mm); err != nil {
						return nil, err
					}

					file, ok := mm["dockerConfigFile"]
					if !ok {
						return nil, fmt.Errorf("missing dockerConfigFile in config")
					}

					return runtime.Identity{
						runtime.IdentityAttributeType: runtime.NewVersionedType("DockerConfig", "v1").String(),
						"dockerConfigFile":            file.(string),
					}, nil
				},
				ResolveFunc: func(ctx context.Context, config runtime.Typed, identity runtime.Identity, credentials map[string]string) (resolved map[string]string, err error) {
					switch identity["hostname"] {
					case "quay.io":
						return map[string]string{
							"username": "test1",
							"password": "bar",
						}, nil
					case "notfound.io":
						return nil, nil
					default:
						return nil, fmt.Errorf("failed access")
					}
				},
			}, nil
		case "ErrorRegistry":
			return RepositoryPlugin{
				RepositoryIdentityFunc: func(config runtime.Typed) (runtime.Identity, error) {
					return runtime.Identity{
						runtime.IdentityAttributeType: "ErrorRegistry",
					}, nil
				},
				ResolveFunc: func(ctx context.Context, config runtime.Typed, identity runtime.Identity, credentials map[string]string) (resolved map[string]string, err error) {
					return nil, fmt.Errorf("some internal plugin error")
				},
			}, nil
		case credentials.AnyConsumerIdentityType.String():
			return RepositoryPlugin{
				RepositoryIdentityFunc: func(config runtime.Typed) (runtime.Identity, error) {
					var mm map[string]interface{}
					if err := json.Unmarshal(config.(*runtime.Raw).Data, &mm); err != nil {
						return nil, err
					}
					serverURL := mm["serverURL"]
					if serverURL == nil {
						return nil, fmt.Errorf("missing serverURL in config")
					}
					purl, err := url.Parse(serverURL.(string))
					if err != nil {
						return nil, err
					}
					return runtime.Identity{
						runtime.IdentityAttributeType:     "HashiCorpVault",
						runtime.IdentityAttributeHostname: purl.Hostname(),
					}, nil
				},
				ResolveFunc: func(_ context.Context, config runtime.Typed, identity runtime.Identity, credentials map[string]string) (resolved map[string]string, err error) {
					var mm map[string]interface{}
					_ = json.Unmarshal(config.(*runtime.Raw).Data, &mm)

					if credentials["role_id"] != "repository.vault.com-role" || credentials["secret_id"] != "repository.vault.com-secret" {
						return nil, fmt.Errorf("failed access")
					}
					if identity["hostname"] != "some-hostname.com" {
						return nil, fmt.Errorf("failed access")
					}

					return map[string]string{
						"something-from-vault-repo": "some-value-from-vault",
					}, nil
				},
			}, nil
		default:
			return nil, fmt.Errorf("unsupported repository type %q", repoType)
		}
	}

	getCredentialPluginsFn := func(ctx context.Context, repoType runtime.Typed) (credentials.CredentialPlugin, error) {
		switch repoType.GetType() {
		case runtime.NewUnversionedType("RecursionTest"):
			return CredentialPlugin{
				ConsumerIdentityTypeAttributes: map[runtime.Type]map[string]func(v any) (string, string){
					runtime.NewUnversionedType("RecursionTest"): {
						"path": func(v any) (string, string) {
							return "path", v.(string)
						},
					},
				},
			}, nil
		case runtime.NewUnversionedType("AWSSecretsManager"):
			return CredentialPlugin{
				ConsumerIdentityTypeAttributes: map[runtime.Type]map[string]func(v any) (string, string){
					runtime.NewUnversionedType("AWSSecretsManager"): {
						"secretId": func(v any) (string, string) {
							return "secretId", v.(string)
						},
					},
				},
				CredentialFunc: func(ctx context.Context, identity runtime.Identity, credentials map[string]string) (map[string]string, error) {
					if identity["secretId"] != "vault-access-creds" {
						return nil, fmt.Errorf("failed access")
					}
					if credentials["roleid"] != "my-role-id" {
						return nil, fmt.Errorf("failed access")
					}
					return map[string]string{
						"role_id":   "myvault.example.com-role",
						"secret_id": "myvault.example.com-secret",
					}, nil
				},
			}, nil
		case runtime.NewUnversionedType("HashiCorpVault"):
			return CredentialPlugin{
				ConsumerIdentityTypeAttributes: map[runtime.Type]map[string]func(v any) (string, string){
					runtime.NewUnversionedType("HashiCorpVault"): {
						"serverURL": func(v any) (string, string) {
							url, _ := url.Parse(v.(string))
							return runtime.IdentityAttributeHostname, url.Hostname()
						},
					},
				},
				CredentialFunc: func(ctx context.Context, identity runtime.Identity, credentials map[string]string) (map[string]string, error) {
					switch identity["hostname"] {
					case "myvault.example.com":
						roleid, secret := credentials["role_id"], credentials["secret_id"]
						if roleid != "myvault.example.com-role" || secret != "myvault.example.com-secret" {
							return nil, fmt.Errorf("failed access")
						}
						return map[string]string{
							"role_id":   "other.vault.com-role",
							"secret_id": "other.vault.com-secret",
						}, nil
					case "other.vault.com":
						roleid, secret := credentials["role_id"], credentials["secret_id"]
						if roleid != "other.vault.com-role" || secret != "other.vault.com-secret" {
							return nil, fmt.Errorf("failed access")
						}
						return map[string]string{
							"username": "foo",
							"password": "bar",
						}, nil
					}

					return map[string]string{
						"vaultSecret": "vault-secret-for-https://" + identity["hostname"] + "/",
					}, nil
				},
			}, nil
		}

		return nil, fmt.Errorf("unsupported repository type %q", repoType)
	}

	graph, err := credentials.ToGraph(t.Context(), config, credentials.Options{
		RepositoryPluginProvider:       credentials.GetRepositoryPluginFn(getPluginRepositoryFn),
		CredentialPluginProvider:       credentials.GetCredentialPluginFn(getCredentialPluginsFn),
		CredentialRepositoryTypeScheme: runtime.NewScheme(runtime.WithAllowUnknown()),
	})
	if err != nil {
		return nil, err
	}
	return graph, nil
}

// TestResolveCredentials ensures credentials are correctly resolved
func TestResolveCredentials(t *testing.T) {
	for _, tc := range []struct {
		name        string
		yaml        string
		identity    runtime.Identity
		expected    map[string]string
		expectedErr require.ErrorAssertionFunc
	}{
		{
			"direct graph resolution",
			testYAML,
			runtime.Identity{
				"type":     "OCIRegistry",
				"hostname": "docker.io",
			},
			map[string]string{
				"username": "foo",
				"password": "bar",
			},
			require.NoError,
		},
		{
			"docker config based resolution",
			testYAML,
			runtime.Identity{
				"type":     "OCIRegistry",
				"hostname": "quay.io",
			},
			map[string]string{
				"username": "test1",
				"password": "bar",
			},
			require.NoError,
		},
		{
			"indirect resolution through repository",
			testYAML,
			runtime.Identity{
				"type":     "SomeCatchAllType",
				"hostname": "some-hostname.com",
			},
			map[string]string{
				"something-from-vault-repo": "some-value-from-vault",
			},
			require.NoError,
		},
		{
			"indirect resolution through repository",
			testYAML,
			runtime.Identity{
				"type":     "OCIRegistry",
				"hostname": "quay.io",
				"path":     "some-owner/some-repo",
			},
			map[string]string{
				"username": "some-owner",
				"password": "abc",
			},
			require.NoError,
		},
		{
			"credential resolution not found handling",
			testYAML,
			runtime.Identity{
				"type":     "OCIRegistry",
				"hostname": "notfound.io",
				"path":     "another-owner/another-repo",
			},
			map[string]string{
				"username": "some-owner",
				"password": "abc",
			},
			require.ErrorAssertionFunc(func(t require.TestingT, err error, i ...interface{}) {
				require.Error(t, err)
				require.ErrorIs(t, err, credentials.ErrNotFound)
			}),
		},
		{
			"plugin resolution error handling",
			testYAML,
			runtime.Identity{
				"type":     "ErrorRegistry",
				"hostname": "quay.io",
				"path":     "some-owner/some-repo",
			},
			map[string]string{
				"username": "some-owner",
				"password": "abc",
			},
			require.ErrorAssertionFunc(func(t require.TestingT, err error, i ...interface{}) {
				require.Error(t, err)
				require.ErrorIs(t, err, credentials.ErrUnknown)
			}),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			graph, err := GetGraph(t, tc.yaml)
			r.NoError(err)
			credsByIdentity, err := graph.Resolve(t.Context(), tc.identity)
			tc.expectedErr(t, err)
			if err != nil {
				return
			}
			r.Equal(tc.expected, credsByIdentity)
		})
	}
}

func TestGraphRendering(t *testing.T) {
	for _, tc := range []struct {
		name        string
		yaml        string
		expectedErr string
	}{
		{
			"direct graph resolution",
			testYAML,
			"",
		},
		{
			"recursive resolution through repository",
			invalidRecursionYAML,
			dag.ErrSelfReference.Error(),
		},
		{
			"recursive resolution through indirect graph dependency",
			`
type: credentials.config.ocm.software
consumers:
  - identity:
      type: RecursionTest
      path: "recursive/path/a"
    credentials:
      - type: RecursionTest
        path: "recursive/path/b"
  - identity:
      type: RecursionTest
      path: "recursive/path/b"
    credentials:
      - type: RecursionTest
        path: "recursive/path/a"
`,
			"adding an edge from path=recursive/path/b,type=RecursionTest to path=recursive/path/a,type=RecursionTest would create a cycle",
		},
		{
			"recursive resolution through indirect graph dependency (resolved via path matching)",
			`
type: credentials.config.ocm.software
consumers:
  - identity:
      type: RecursionTest
      path: "recursive/path/a"
    credentials:
      - type: RecursionTest
        path: "recursive/path/b"
  - identity:
      type: RecursionTest
      path: "recursive/path/b"
    credentials:
      - type: RecursionTest
        path: "recursive/path/*"
`,
			"adding an edge from path=recursive/path/b,type=RecursionTest to path=recursive/path/*,type=RecursionTest would create a cycle",
		},
		{
			"recursive resolution through multiple edges leads to successful merged resolution",
			`
type: credentials.config.ocm.software
consumers:
  - identity:
      type: RecursionTest
      path: "recursive/path/abc"
    credentials:
      - type: RecursionTest
        path: "recursive/path/a"
      - type: RecursionTest
        path: "recursive/path/b"
  - identity:
      type: RecursionTest
      path: "recursive/path/a"
    credentials:
      - type: Credentials/v1
        properties:
          username: "abc"
  - identity:
      type: RecursionTest
      path: "recursive/path/b"
    credentials:
      - type: Credentials/v1
        properties:
          password: "def"
`,
			"",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			_, err := GetGraph(t, tc.yaml)
			if tc.expectedErr != "" {
				r.Errorf(err, "Expected error")
				r.ErrorContains(err, tc.expectedErr)
			} else {
				r.NoError(err, "Failed to get graph")
			}
		})
	}
}

func TestResolutionErrors(t *testing.T) {
	id := runtime.Identity{
		"type": "not exists",
	}

	r := require.New(t)
	g, err := credentials.ToGraph(t.Context(), &credentialruntime.Config{}, credentials.Options{})
	require.NoError(t, err)
	creds, err := g.Resolve(t.Context(), id)
	r.Empty(creds)
	r.Error(err)
	r.ErrorIs(err, credentials.ErrNoDirectCredentials)
	r.ErrorContains(err, fmt.Sprintf("failed to resolve credentials for identity %q: credentials not found", id.String()))
	r.ErrorContains(err, "failed to match any node: no direct credentials found in graph")

	g, err = credentials.ToGraph(t.Context(), &credentialruntime.Config{}, credentials.Options{
		RepositoryPluginProvider: credentials.GetRepositoryPluginFn(func(ctx context.Context, repoType runtime.Typed) (credentials.RepositoryPlugin, error) {
			return RepositoryPlugin{
				RepositoryIdentityFunc: func(config runtime.Typed) (runtime.Identity, error) {
					return runtime.Identity{}, nil
				},
			}, nil
		}),
	})
	r.NoError(err)
	creds, err = g.Resolve(t.Context(), id)
	r.Empty(creds)
	r.Error(err)
	r.ErrorIs(err, credentials.ErrNoIndirectCredentials)
	r.ErrorContains(err, fmt.Sprintf("failed to resolve credentials for identity %q: credentials not found", id.String()))
	r.ErrorContains(err, "no indirect credentials found in graph")
}
