package dir_test

import (
	"github.com/stretchr/testify/assert"
	"ocm.software/open-component-model/bindings/go/input/dir"
	v1 "ocm.software/open-component-model/bindings/go/input/dir/spec/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
	"testing"
)

func TestGetV1DirBlob_EmptyPath(t *testing.T) {
	// Create v1.File spec with empty path
	dirSpec := v1.Dir{
		Type: runtime.NewUnversionedType("file"),
		Path: "",
	}

	// Get blob should fail
	blob, err := dir.GetV1DirBlob(dirSpec)
	assert.Error(t, err)
	assert.Nil(t, blob)
}
