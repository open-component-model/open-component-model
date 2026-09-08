package v1alpha1_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/status"
)

// Compile-time contract assertions for Discovery against the interfaces
// shared by all OCM kubernetes objects.
var (
	_ v1alpha1.OCMK8SObject           = &v1alpha1.Discovery{}
	_ status.IdentifiableClientObject = &v1alpha1.Discovery{}
)

func TestDiscoveryContracts(t *testing.T) {
	r := require.New(t)
	obj := &v1alpha1.Discovery{}
	var k8s v1alpha1.OCMK8SObject = obj
	r.Same(obj, k8s)
	var ident status.IdentifiableClientObject = obj
	r.Same(obj, ident)
}
