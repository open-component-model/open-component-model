package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

var (
	DownloadWgetResourceV1alpha1 = runtime.NewVersionedType(DownloadWgetResourceType, Version)
	HTTPStreamingV1alpha1        = runtime.NewVersionedType(HTTPStreamingType, Version)
)

func init() {
	Scheme.MustRegisterWithAlias(&DownloadWgetResource{}, DownloadWgetResourceV1alpha1)
	Scheme.MustRegisterWithAlias(&HTTPStreaming{}, HTTPStreamingV1alpha1)
}
