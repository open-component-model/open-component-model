package credentials_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	credentials2 "ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/credentials/internal/static"
	credentialruntime "ocm.software/open-component-model/bindings/go/credentials/spec/config/runtime"
	v1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/dag"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Sample YAML from your example
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

func GetGraph(t testing.TB, yaml string) (*credentials2.Graph, error) {
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

	getPluginRepositoryFn := func(ctx context.Context, repoType runtime.Typed) (credentials2.RepositoryPlugin, error) {
		switch repoType.GetType().String() {
		case "OCIRegistry":
			return static.RepositoryPlugin{
				RepositoryConfigTypes: []runtime.Type{runtime.NewVersionedType("DockerConfig", "v1")},
				RepositoryIdentityFunc: func(config runtime.Typed) (runtime.Identity, error) {
					var mm map[string]interface{}
					if err := json.Unmarshal(config.(*runtime.Raw).Data, &mm); err != nil {
						return nil, err
					}

					return runtime.Identity{
						runtime.IdentityAttributeType: runtime.NewVersionedType("DockerConfig", "v1").String(),
						"dockerConfigFile":            mm["dockerConfigFile"].(string),
					}, nil
				},
				ResolveFunc: func(ctx context.Context, config runtime.Typed, identity runtime.Identity, credentials map[string]string) (resolved map[string]string, err error) {
					switch identity["hostname"] {
					case "quay.io":
						return map[string]string{
							"username": "foo",
							"password": "bar",
						}, nil
					default:
						return nil, fmt.Errorf("failed access")
					}
				},
			}, nil
		case credentials2.AnyCredentialType.String():
			return static.RepositoryPlugin{
				RepositoryConfigTypes: []runtime.Type{runtime.NewUnversionedType("HashiCorpVault")},
				RepositoryIdentityFunc: func(config runtime.Typed) (runtime.Identity, error) {
					var mm map[string]interface{}
					if err := json.Unmarshal(config.(*runtime.Raw).Data, &mm); err != nil {
						return nil, err
					}
					purl, err := url.Parse(mm["serverURL"].(string))
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

	getCredentialPluginsFn := func(ctx context.Context, repoType runtime.Typed) (credentials2.CredentialPlugin, error) {
		switch repoType.GetType() {
		case runtime.NewUnversionedType("RecursionTest"):
			return static.CredentialPlugin{
				ConsumerIdentityTypeAttributes: map[runtime.Type]map[string]func(v any) (string, string){
					runtime.NewUnversionedType("RecursionTest"): {
						"path": func(v any) (string, string) {
							return "path", v.(string)
						},
					},
				},
			}, nil
		case runtime.NewUnversionedType("AWSSecretsManager"):
			return static.CredentialPlugin{
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
			return static.CredentialPlugin{
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
							"username": "my-docker-user",
							"password": "my-login-to-docker",
						}, nil
					}

					return map[string]string{
						"vaultSecret": "vault-secret-for-https://" + identity["hostname"] + "/",
					}, nil
				},
			}, nil
		}

		return nil, nil
	}

	graph, err := credentials2.ToGraph(t.Context(), config, credentials2.Options{
		GetRepositoryPluginFn:          getPluginRepositoryFn,
		GetCredentialPluginFn:          getCredentialPluginsFn,
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
		name     string
		yaml     string
		identity runtime.Identity
		expected map[string]string
	}{
		{
			"direct graph resolution",
			testYAML,
			runtime.Identity{
				"type":     "OCIRegistry",
				"hostname": "docker.io",
			},
			map[string]string{
				"username": "my-docker-user",
				"password": "my-login-to-docker",
			},
		},
		{
			"docker config based resolution",
			testYAML,
			runtime.Identity{
				"type":     "OCIRegistry",
				"hostname": "quay.io",
			},
			map[string]string{
				"username": "foo",
				"password": "bar",
			},
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
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			graph, err := GetGraph(t, tc.yaml)
			r.NoError(err)
			credsByIdentity, err := graph.Resolve(t.Context(), tc.identity)
			r.NoError(err, "Failed to resolveDirect credentials")
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			_, err := GetGraph(t, tc.yaml)
			if tc.expectedErr != "" {
				r.Errorf(err, "Expected error")
				r.ErrorContains(err, tc.expectedErr)
			} else {
				r.NoError(err, "Failed to resolveDirect credentials")
			}
		})
	}
}
