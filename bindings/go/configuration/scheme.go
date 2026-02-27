package configuration

import (
	extractv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/extract/v1alpha1/spec"
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	genericspecv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/http/v1alpha1/spec"
	ocmv1 "ocm.software/open-component-model/bindings/go/configuration/ocm/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

func init() {
	if err := Register(Scheme); err != nil {
		panic(err)
	}
}

func Register(scheme *runtime.Scheme) error {
	return scheme.RegisterSchemes(
		genericspecv1.Scheme,
		filesystemv1alpha1.Scheme,
		httpv1alpha1.Scheme,
		extractv1alpha1.Scheme,
		ocmv1.Scheme,
	)
}
