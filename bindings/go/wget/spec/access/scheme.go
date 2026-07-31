package access

import (
	"strings"

	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
)

const (
	WgetConsumerType = "Wget"
)

var V1VersionedType = runtime.NewVersionedType(WgetConsumerType, v1.Version)

var Scheme = runtime.NewScheme()

func init() {
	MustAddToScheme(Scheme)
}

func MustAddToScheme(scheme *runtime.Scheme) {
	wget := &v1.Wget{}

	lowerCaseConsumerType := strings.ToLower(WgetConsumerType)
	scheme.MustRegisterWithAlias(wget,
		V1VersionedType,
		runtime.NewUnversionedType(WgetConsumerType),
		runtime.NewVersionedType(lowerCaseConsumerType, v1.Version),
		runtime.NewUnversionedType(lowerCaseConsumerType),
	)
}
