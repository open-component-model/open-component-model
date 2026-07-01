package access

import (
	v1 "ocm.software/open-component-model/bindings/go/maven/spec/access/v1"
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

// MustAddToScheme registers the Maven access type and its legacy aliases.
func MustAddToScheme(scheme *runtime.Scheme) {
	maven := &v1.Maven{}
	scheme.MustRegisterWithAlias(maven,
		runtime.NewVersionedType(v1.Type, v1.Version),                 // Maven/v1
		runtime.NewUnversionedType(v1.Type),                           // Maven
		runtime.NewVersionedType(v1.LegacyType, v1.LegacyTypeVersion), // maven/v1
		runtime.NewUnversionedType(v1.LegacyType),                     // maven
	)
}
