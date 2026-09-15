package access

import (
	"ocm.software/open-component-model/bindings/go/pypi/spec/access/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// PyPIRepositoryConsumerType is the credential consumer identity type used to
// resolve credentials for a PyPI repository.
const PyPIRepositoryConsumerType = "PyPIRepository"

// Scheme is the access scheme containing the PyPI access type and its aliases.
var Scheme = runtime.NewScheme()

func init() {
	MustAddToScheme(Scheme)
}

// MustAddToScheme registers the PyPI access type. "pypi/v1alpha1" is the
// canonical form; "PyPI/v1alpha1" is accepted so descriptors may use the
// capitalised spelling the other access types (Helm, Wget, Maven) use.
func MustAddToScheme(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&v1alpha1.PyPI{},
		runtime.NewVersionedType(v1alpha1.Type, v1alpha1.Version), // pypi/v1alpha1
		runtime.NewVersionedType("PyPI", v1alpha1.Version),        // PyPI/v1alpha1
	)
}
