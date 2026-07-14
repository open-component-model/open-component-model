package access

import (
	"strings"

	"ocm.software/open-component-model/bindings/go/runtime"
	v1 "ocm.software/open-component-model/bindings/go/s3/spec/access/v1"
)

const (
	// AccessType is the OCM type name for the S3 access type.
	AccessType = "S3"

	// S3BucketConsumerType is the credential consumer identity type for an S3
	// bucket. It is distinct from the access type name (see the HelmChartRepository
	// consumer type for the equivalent pattern) so OCM credential providers can
	// resolve access-key/secret (or session-token) authentication for a download.
	S3BucketConsumerType = "S3Bucket"
)

var V1VersionedType = runtime.NewVersionedType(AccessType, v1.Version)

var Scheme = runtime.NewScheme()

func init() {
	MustAddToScheme(Scheme)
}

func MustAddToScheme(scheme *runtime.Scheme) {
	spec := &v1.S3{}

	lowerCaseAccessType := strings.ToLower(AccessType)
	scheme.MustRegisterWithAlias(spec,
		V1VersionedType,
		runtime.NewUnversionedType(AccessType),
		runtime.NewVersionedType(lowerCaseAccessType, v1.Version),
		runtime.NewUnversionedType(lowerCaseAccessType),
	)
}
