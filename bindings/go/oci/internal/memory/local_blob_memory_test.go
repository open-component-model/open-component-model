package memory

import (
	"testing"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInMemory_AddAndGet(t *testing.T) {
	mem := NewInMemory()
	ref := "test-ref"
	desc := ociImageSpecV1.Descriptor{
		MediaType: "application/vnd.oci.image.layer.v1.tar+gzip",
		Digest:    "sha256:1234567890",
		Size:      1024,
	}

	// Test adding a descriptor
	mem.Add(ref, desc)
	descs := mem.Get(ref)
	require.Len(t, descs, 1)
	assert.Equal(t, desc, descs[0])

	// Test adding another descriptor to the same reference
	desc2 := ociImageSpecV1.Descriptor{
		MediaType: "application/vnd.oci.image.config.v1+json",
		Digest:    "sha256:0987654321",
		Size:      512,
	}
	mem.Add(ref, desc2)
	descs = mem.Get(ref)
	require.Len(t, descs, 2)
	assert.Equal(t, desc, descs[0])
	assert.Equal(t, desc2, descs[1])
}

func TestInMemory_GetNonExistent(t *testing.T) {
	mem := NewInMemory()
	descs := mem.Get("non-existent")
	assert.Empty(t, descs)
}

func TestInMemory_Delete(t *testing.T) {
	mem := NewInMemory()
	ref := "test-ref"
	desc := ociImageSpecV1.Descriptor{
		MediaType: "application/vnd.oci.image.layer.v1.tar+gzip",
		Digest:    "sha256:1234567890",
		Size:      1024,
	}

	// Add a descriptor
	mem.Add(ref, desc)
	descs := mem.Get(ref)
	require.Len(t, descs, 1)

	// Delete the reference
	mem.Delete(ref)
	descs = mem.Get(ref)
	assert.Empty(t, descs)

	// Test deleting non-existent reference (should not panic)
	mem.Delete("non-existent")
}

func TestInMemory_ConcurrentAccess(t *testing.T) {
	mem := NewInMemory()
	ref := "test-ref"
	desc := ociImageSpecV1.Descriptor{
		MediaType: "application/vnd.oci.image.layer.v1.tar+gzip",
		Digest:    "sha256:1234567890",
		Size:      1024,
	}

	// Test concurrent Add operations
	done := make(chan bool)
	for i := 0; i < 100; i++ {
		go func() {
			mem.Add(ref, desc)
			done <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 100; i++ {
		<-done
	}

	// Verify all descriptors were added
	descs := mem.Get(ref)
	assert.Len(t, descs, 100)
}
