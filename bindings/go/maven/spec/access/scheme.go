package access

import (
	"ocm.software/open-component-model/bindings/go/maven/spec/access/v2alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// MavenRepositoryConsumerType is the credential consumer identity type used to
// resolve credentials for a Maven repository.
const MavenRepositoryConsumerType = "MavenRepository"

// Scheme is the access scheme containing the Maven access type and its aliases.
var Scheme = runtime.NewScheme()

func init() {
	MustAddToScheme(Scheme)
}

// MustAddToScheme registers the Maven access type. "maven/v2alpha1" is the
// canonical form; "Maven/v2alpha1" is accepted so descriptors may use the
// capitalised spelling the other access types (Helm, Wget) use.
func MustAddToScheme(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&v2alpha1.Maven{},
		runtime.NewVersionedType(v2alpha1.Type, v2alpha1.Version), // maven/v2alpha1
		runtime.NewVersionedType("Maven", v2alpha1.Version),       // Maven/v2alpha1
	)
}
