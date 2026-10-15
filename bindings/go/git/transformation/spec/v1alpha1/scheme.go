package v1alpha1

import "ocm.software/open-component-model/bindings/go/runtime"

var Scheme = runtime.NewScheme()

var (
	GetGitResourceV1alpha1 = runtime.NewVersionedType(GetGitResourceType, Version)
	AddGitResourceV1alpha1 = runtime.NewVersionedType(AddGitResourceType, Version)
)

func init() {
	Scheme.MustRegisterWithAlias(&GetGitResource{}, GetGitResourceV1alpha1)
	Scheme.MustRegisterWithAlias(&AddGitResource{}, AddGitResourceV1alpha1)
}
