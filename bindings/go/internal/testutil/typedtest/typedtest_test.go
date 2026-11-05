package typedtest_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/internal/testutil/typedtest"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestUnregistered(t *testing.T) {
	r := require.New(t)
	typed := &typedtest.Unregistered{}

	r.Equal(runtime.NewUnversionedType("Unknown"), typed.GetType())

	typed.SetType(runtime.NewVersionedType("Other", "v1"))
	r.Equal(runtime.NewUnversionedType("Unknown"), typed.GetType(), "the type must not change")

	r.Equal(typed, typed.DeepCopyTyped())

	scheme := runtime.NewScheme()
	_, err := scheme.TypeForPrototype(typed)
	r.Error(err, "no scheme knows the type")
}
