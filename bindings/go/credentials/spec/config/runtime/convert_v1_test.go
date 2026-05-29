package runtime

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestConvertIdentities_LegacyOCIRepositoryType(t *testing.T) {
	identities := []runtime.Identity{
		{
			runtime.IdentityAttributeType:     "OCIRepository",
			runtime.IdentityAttributeHostname: "localhost",
			runtime.IdentityAttributePort:     "5000",
			runtime.IdentityAttributeScheme:   "http",
		},
	}

	got := convertIdentities(identities)

	assert.Equal(t, "OCIRegistry", got[0][runtime.IdentityAttributeType],
		"OCIRepository should be normalized to OCIRegistry")
	assert.Equal(t, "localhost", got[0][runtime.IdentityAttributeHostname])
	assert.Equal(t, "5000", got[0][runtime.IdentityAttributePort])
	assert.Equal(t, "http", got[0][runtime.IdentityAttributeScheme])
}

func TestConvertIdentities_CurrentOCIRegistryType(t *testing.T) {
	identities := []runtime.Identity{
		{
			runtime.IdentityAttributeType:     "OCIRegistry",
			runtime.IdentityAttributeHostname: "ghcr.io",
		},
	}

	got := convertIdentities(identities)

	assert.Equal(t, "OCIRegistry", got[0][runtime.IdentityAttributeType],
		"OCIRegistry should remain unchanged")
}

func TestConvertIdentities_UnrelatedTypeUnchanged(t *testing.T) {
	identities := []runtime.Identity{
		{
			runtime.IdentityAttributeType: "HelmChartRepository",
		},
	}

	got := convertIdentities(identities)

	assert.Equal(t, "HelmChartRepository", got[0][runtime.IdentityAttributeType],
		"unrelated types should pass through unchanged")
}

func TestConvertIdentities_OriginalNotMutated(t *testing.T) {
	original := runtime.Identity{
		runtime.IdentityAttributeType:     "OCIRepository",
		runtime.IdentityAttributeHostname: "localhost",
	}
	identities := []runtime.Identity{original}

	convertIdentities(identities)

	assert.Equal(t, "OCIRepository", original[runtime.IdentityAttributeType],
		"original identity must not be mutated")
}
