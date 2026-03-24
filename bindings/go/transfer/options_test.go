package transfer

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultOptions(t *testing.T) {
	var o Options
	assert.Equal(t, CopyModeLocalBlobResources, o.CopyMode)
	assert.Equal(t, UploadAsDefault, o.UploadType)
	assert.False(t, o.Recursive)
}

func TestWithCopyMode(t *testing.T) {
	var o Options
	WithCopyMode(CopyModeAllResources)(&o)
	assert.Equal(t, CopyModeAllResources, o.CopyMode)
}

func TestWithRecursive(t *testing.T) {
	var o Options
	WithRecursive(true)(&o)
	assert.True(t, o.Recursive)
}

func TestWithUploadType(t *testing.T) {
	var o Options
	WithUploadType(UploadAsOciArtifact)(&o)
	assert.Equal(t, UploadAsOciArtifact, o.UploadType)
}
