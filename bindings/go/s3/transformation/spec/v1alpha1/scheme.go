package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

var DownloadS3ResourceV1alpha1 = runtime.NewVersionedType(DownloadS3ResourceType, Version)

func init() {
	Scheme.MustRegisterWithAlias(&DownloadS3Resource{}, DownloadS3ResourceV1alpha1)
}
