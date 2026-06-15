package runtime

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const convertTestYAML = `
type: credentials.config.ocm.software/v1
repositories:
- repository:
    type: DockerConfig/v1
    dockerConfigFile: "~/.docker/config.json"
consumers:
- identity:
    type: OCIRegistry
    hostname: ghcr.io
  credentials:
  - type: Credentials/v1
    properties:
      username: admin
      password: secret
- identity:
    type: HashiCorpVault
    hostname: vault.example.com
  credentials:
  - type: Credentials/v1
    properties:
      token: my-token
`

func parseV1Config(t *testing.T, yaml string) *v1.Config {
	t.Helper()
	scheme := runtime.NewScheme()
	v1.MustRegister(scheme)

	var config v1.Config
	require.NoError(t, scheme.Decode(strings.NewReader(yaml), &config))
	return &config
}

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	v1.MustRegister(s)
	return s
}

func TestConvertToV1_RoundTrip(t *testing.T) {
	original := parseV1Config(t, convertTestYAML)

	internal := ConvertFromV1(original)

	// Assert intermediate internal representation has expected shape.
	require.Len(t, internal.Repositories, 1)
	require.Len(t, internal.Consumers, 2)
	assert.Equal(t, "ghcr.io", internal.Consumers[0].Identities[0]["hostname"])
	assert.Equal(t, "vault.example.com", internal.Consumers[1].Identities[0]["hostname"])
	// Credentials are stored as runtime.Typed (interface), backed by *runtime.Raw.
	raw, ok := internal.Consumers[0].Credentials[0].(*runtime.Raw)
	require.True(t, ok, "expected *runtime.Raw, got %T", internal.Consumers[0].Credentials[0])
	assert.Contains(t, string(raw.Data), "admin")

	result, err := ConvertToV1(testScheme(t), internal)
	require.NoError(t, err)

	assert.Equal(t, original.Type, result.Type)
	assert.Equal(t, len(original.Repositories), len(result.Repositories))
	assert.Equal(t, original.Repositories[0].Repository.Data, result.Repositories[0].Repository.Data)
	assert.Equal(t, len(original.Consumers), len(result.Consumers))

	for i, consumer := range original.Consumers {
		assert.Equal(t, consumer.Identities, result.Consumers[i].Identities)
		for j, cred := range consumer.Credentials {
			assert.Equal(t, cred.Data, result.Consumers[i].Credentials[j].Data)
		}
	}
}

func TestConvertToV1_EmptyConfig(t *testing.T) {
	original := parseV1Config(t, `
type: credentials.config.ocm.software/v1
`)
	internal := ConvertFromV1(original)

	result, err := ConvertToV1(testScheme(t), internal)
	require.NoError(t, err)
	assert.Empty(t, result.Repositories)
	assert.Empty(t, result.Consumers)
}

func TestConvertToV1_NonRawTyped(t *testing.T) {
	internal := &Config{
		Type: runtime.NewVersionedType("credentials.config.ocm.software", "v1"),
		Consumers: []Consumer{
			{
				Identities: []runtime.Identity{{"type": "test"}},
				Credentials: []runtime.Typed{&v1.DirectCredentials{
					Type:       runtime.NewVersionedType("Credentials", "v1"),
					Properties: map[string]string{"username": "admin", "password": "secret"},
				}},
			},
		},
	}

	result, err := ConvertToV1(testScheme(t), internal)
	require.NoError(t, err)
	require.Len(t, result.Consumers, 1)
	require.Len(t, result.Consumers[0].Credentials, 1)

	data := string(result.Consumers[0].Credentials[0].Data)
	assert.Contains(t, data, `"username":"admin"`)
	assert.Contains(t, data, `"password":"secret"`)
	assert.Equal(t, runtime.NewVersionedType("Credentials", "v1"), result.Consumers[0].Credentials[0].Type)
}

func TestConvertToV1_UnregisteredTypeErrors(t *testing.T) {
	internal := &Config{
		Type: runtime.NewVersionedType("credentials.config.ocm.software", "v1"),
		Consumers: []Consumer{
			{
				Identities:  []runtime.Identity{{"type": "test"}},
				Credentials: []runtime.Typed{&mockTyped{name: "unknown", typ: runtime.NewVersionedType("Unknown", "v1")}},
			},
		},
	}

	_, err := ConvertToV1(testScheme(t), internal)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "credential at index 0")
}
