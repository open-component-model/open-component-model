package v1alpha1

import "ocm.software/open-component-model/bindings/go/runtime"

var Scheme = runtime.NewScheme()

const Version = "v1alpha1"

var (
	CopyLocalBlobVersionV1alpha1 = runtime.NewVersionedType(CopyLocalBlobVersionType, Version)
)

func init() {
	Scheme.MustRegisterWithAlias(&CopyLocalBlob{}, CopyLocalBlobVersionV1alpha1)
}
