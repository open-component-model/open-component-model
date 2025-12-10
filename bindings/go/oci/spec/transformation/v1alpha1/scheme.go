package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

func init() {
	Scheme.MustRegisterWithAlias(
		&oci.DownloadComponentTransformation{},
		runtime.NewVersionedType(oci.DownloadComponentTransformationType, Version),
	)
	Scheme.MustRegisterWithAlias(
		&ctf.DownloadComponentTransformation{},
		runtime.NewVersionedType(ctf.DownloadComponentTransformationType, Version),
	)
}
