package resolution

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
)

func TestBuildCacheKeyHashKeyGeneration(t *testing.T) {
	t.Run("consistent key with different field ordering", func(t *testing.T) {
		configHash := []byte("test-config-hash")
		component := "test-component"
		version := "v1.0.0"

		spec1 := &ociv1.Repository{
			BaseUrl: "localhost:5000/test",
		}

		spec2 := &ociv1.Repository{
			BaseUrl: "localhost:5000/test",
		}

		key1, err := buildCacheKey(configHash, spec1, component, version)
		require.NoError(t, err)

		key2, err := buildCacheKey(configHash, spec2, component, version)
		require.NoError(t, err)

		assert.Equal(t, key1, key2, "cache keys should be identical for same spec")
	})

	t.Run("different keys for different specs", func(t *testing.T) {
		configHash := []byte("test-config-hash")
		component := "test-component"
		version := "v1.0.0"

		spec1 := &ociv1.Repository{
			BaseUrl: "localhost:5000/test1",
		}

		spec2 := &ociv1.Repository{
			BaseUrl: "localhost:5000/test2",
		}

		key1, err := buildCacheKey(configHash, spec1, component, version)
		require.NoError(t, err)

		key2, err := buildCacheKey(configHash, spec2, component, version)
		require.NoError(t, err)

		assert.NotEqual(t, key1, key2, "cache keys should differ for different specs")
	})

	t.Run("different keys for different components", func(t *testing.T) {
		configHash := []byte("test-config-hash")
		spec := &ociv1.Repository{
			BaseUrl: "localhost:5000/test",
		}

		key1, err := buildCacheKey(configHash, spec, "component1", "v1.0.0")
		require.NoError(t, err)

		key2, err := buildCacheKey(configHash, spec, "component2", "v1.0.0")
		require.NoError(t, err)

		assert.NotEqual(t, key1, key2, "cache keys should differ for different components")
	})

	t.Run("different keys for different versions", func(t *testing.T) {
		configHash := []byte("test-config-hash")
		spec := &ociv1.Repository{
			BaseUrl: "localhost:5000/test",
		}
		component := "test-component"

		key1, err := buildCacheKey(configHash, spec, component, "v1.0.0")
		require.NoError(t, err)

		key2, err := buildCacheKey(configHash, spec, component, "v2.0.0")
		require.NoError(t, err)

		assert.NotEqual(t, key1, key2, "cache keys should differ for different versions")
	})

	t.Run("different keys for different config hashes", func(t *testing.T) {
		spec := &ociv1.Repository{
			BaseUrl: "localhost:5000/test",
		}
		component := "test-component"
		version := "v1.0.0"

		key1, err := buildCacheKey([]byte("config1"), spec, component, version)
		require.NoError(t, err)

		key2, err := buildCacheKey([]byte("config2"), spec, component, version)
		require.NoError(t, err)

		assert.NotEqual(t, key1, key2, "cache keys should differ for different config hashes")
	})

	t.Run("key format is 16 hex characters", func(t *testing.T) {
		configHash := []byte("test-config-hash")
		spec := &ociv1.Repository{
			BaseUrl: "localhost:5000/test",
		}
		component := "test-component"
		version := "v1.0.0"

		key, err := buildCacheKey(configHash, spec, component, version)
		require.NoError(t, err)

		assert.Len(t, key, 16, "FNV-1a 64-bit hash should produce 16 hex characters")
		assert.Regexp(t, "^[0-9a-f]{16}$", key, "key should be 16 lowercase hex characters")
	})
}
