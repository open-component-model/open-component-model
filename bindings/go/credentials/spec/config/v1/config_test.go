package v1_test

import (
	"encoding/json/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Consumer pulls its members out with a nested unmarshal, which must keep the options the
// outer decoder was given. A .ocmconfig read through sigs.k8s.io/yaml keeps the author's
// member spelling verbatim, and dropping the identities here leaves an anonymous pull
// instead of an error.
func TestConsumer_UnmarshalJSONFrom_KeepsDecodeOptions(t *testing.T) {
	data := []byte(`{
		"type": "credentials.config.ocm.software/v1",
		"consumers": [{
			"Identity": {"type": "OCIRegistry", "hostname": "ghcr.io"},
			"Credentials": [{"type": "Credentials/v1", "properties": {"username": "u"}}]
		}]
	}`)

	var cfg v1.Config
	require.NoError(t, json.Unmarshal(data, &cfg, runtime.JSONDecodeOptions))

	require.Len(t, cfg.Consumers, 1)
	require.Len(t, cfg.Consumers[0].Identities, 1)
	assert.Equal(t, "ghcr.io", cfg.Consumers[0].Identities[0]["hostname"])
	require.Len(t, cfg.Consumers[0].Credentials, 1)
	assert.Equal(t, runtime.NewVersionedType("Credentials", "v1"), cfg.Consumers[0].Credentials[0].GetType())
}
