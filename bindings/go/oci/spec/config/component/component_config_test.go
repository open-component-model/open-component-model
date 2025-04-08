package component

import (
	"encoding/json"
	"testing"

	"github.com/opencontainers/go-digest"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
)

func TestCreateComponentConfig(t *testing.T) {
	// New a test descriptor
	testDescriptor := v1.Descriptor{
		MediaType: "application/vnd.ocm.software/component-descriptor",
		Digest:    digest.FromString("test"),
		Size:      100,
	}

	// Test successful creation
	t.Run("successful creation", func(t *testing.T) {
		encoded, descriptor, err := New(testDescriptor)
		assert.NoError(t, err)
		assert.NotNil(t, encoded)
		assert.NotNil(t, descriptor)

		// Verify descriptor properties
		assert.Equal(t, MediaType, descriptor.MediaType)
		assert.Equal(t, int64(len(encoded)), descriptor.Size)

		// Verify the encoded config can be unmarshaled
		var config Config
		err = json.Unmarshal(encoded, &config)
		assert.NoError(t, err)
		assert.Equal(t, &testDescriptor, config.ComponentDescriptorLayer)
	})

	// Test with empty descriptor
	t.Run("empty descriptor", func(t *testing.T) {
		encoded, descriptor, err := New(v1.Descriptor{})
		assert.NoError(t, err)
		assert.NotNil(t, encoded)
		assert.NotNil(t, descriptor)

		// Verify descriptor properties
		assert.Equal(t, MediaType, descriptor.MediaType)
		assert.Equal(t, int64(len(encoded)), descriptor.Size)

		// Verify the encoded config can be unmarshaled
		var config Config
		err = json.Unmarshal(encoded, &config)
		assert.NoError(t, err)
		assert.Equal(t, &v1.Descriptor{}, config.ComponentDescriptorLayer)
	})
}
