package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

var DownloadWgetResourceV1alpha1 = runtime.NewVersionedType(DownloadWgetResourceType, Version)

func init() {
	Scheme.MustRegisterWithAlias(&DownloadWgetResource{}, DownloadWgetResourceV1alpha1)
}
