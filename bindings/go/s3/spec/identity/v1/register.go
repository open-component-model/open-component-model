package v1

import "ocm.software/open-component-model/bindings/go/runtime"

var scheme = runtime.NewScheme()

func init() {
	MustRegisterIdentityType(scheme)
}

// MustRegisterIdentityType registers S3Bucket/v1 (with unversioned alias) in the given scheme.
func MustRegisterIdentityType(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&S3BucketIdentity{},
		VersionedType,
		Type, // backward-compat alias
	)
}
