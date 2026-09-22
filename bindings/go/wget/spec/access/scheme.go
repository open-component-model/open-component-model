package access

import (
	"strings"

	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
)

const (
	WgetConsumerType = "Wget"

	// HTTPConsumerType is an alias under which the Wget access type can be declared
	HTTPConsumerType = "HTTP"
)

var V1VersionedType = runtime.NewVersionedType(WgetConsumerType, v1.Version)

var Scheme = runtime.NewScheme()

func init() {
	MustAddToScheme(Scheme)
}

func MustAddToScheme(scheme *runtime.Scheme) {
	wget := &v1.Wget{}

	lowerCaseConsumerType := strings.ToLower(WgetConsumerType)
	lowerCaseHTTPConsumerType := strings.ToLower(HTTPConsumerType)
	scheme.MustRegisterWithAlias(wget,
		V1VersionedType,
		runtime.NewUnversionedType(WgetConsumerType),
		runtime.NewVersionedType(lowerCaseConsumerType, v1.Version),
		runtime.NewUnversionedType(lowerCaseConsumerType),
		runtime.NewVersionedType(HTTPConsumerType, v1.Version),
		runtime.NewUnversionedType(HTTPConsumerType),
		runtime.NewVersionedType(lowerCaseHTTPConsumerType, v1.Version),
		runtime.NewUnversionedType(lowerCaseHTTPConsumerType),
	)
}
