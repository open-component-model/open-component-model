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

	result, err := ConvertToV1(internal)
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

	result, err := ConvertToV1(internal)
	require.NoError(t, err)
	assert.Empty(t, result.Repositories)
	assert.Empty(t, result.Consumers)
}

func TestConvertToV1_ErrorOnNonRaw(t *testing.T) {
	internal := &Config{
		Type: runtime.NewVersionedType("credentials.config.ocm.software", "v1"),
		Consumers: []Consumer{
			{
				Identities:  []runtime.Identity{{"type": "test"}},
				Credentials: []runtime.Typed{&mockTyped{name: "not-raw"}},
			},
		},
	}

	_, err := ConvertToV1(internal)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected type")
	assert.Contains(t, err.Error(), "mockTyped")
}
