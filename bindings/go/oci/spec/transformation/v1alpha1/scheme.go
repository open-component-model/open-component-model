package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

var (
	OCIGetComponentVersionV1alpha1 = runtime.NewVersionedType(OCIGetComponentVersionType, Version)
	OCIAddComponentVersionV1alpha1 = runtime.NewVersionedType(OCIAddComponentVersionType, Version)
	OCIAddLocalResourceV1alpha1    = runtime.NewVersionedType(OCIAddLocalResourceType, Version)
	OCIGetLocalResourceV1alpha1    = runtime.NewVersionedType(OCIGetLocalResourceType, Version)
	GetOCIArtifactV1alpha1         = runtime.NewVersionedType(GetOCIArtifactType, Version)
	AddOCIArtifactV1alpha1         = runtime.NewVersionedType(AddOCIArtifactType, Version)
	CTFGetComponentVersionV1alpha1 = runtime.NewVersionedType(CTFGetComponentVersionType, Version)
	CTFAddComponentVersionV1alpha1 = runtime.NewVersionedType(CTFAddComponentVersionType, Version)
	CTFAddLocalResourceV1alpha1    = runtime.NewVersionedType(CTFAddLocalResourceType, Version)
	CTFGetLocalResourceV1alpha1    = runtime.NewVersionedType(CTFGetLocalResourceType, Version)
)

func init() {
	Scheme.MustRegisterWithAlias(&OCIGetComponentVersion{}, OCIGetComponentVersionV1alpha1)
	Scheme.MustRegisterWithAlias(&OCIAddComponentVersion{}, OCIAddComponentVersionV1alpha1)
	Scheme.MustRegisterWithAlias(&OCIAddLocalResource{}, OCIAddLocalResourceV1alpha1)
	Scheme.MustRegisterWithAlias(&OCIGetLocalResource{}, OCIGetLocalResourceV1alpha1)
	Scheme.MustRegisterWithAlias(&GetOCIArtifact{}, GetOCIArtifactV1alpha1)
	Scheme.MustRegisterWithAlias(&AddOCIArtifact{}, AddOCIArtifactV1alpha1)
	Scheme.MustRegisterWithAlias(&CTFGetComponentVersion{}, CTFGetComponentVersionV1alpha1)
	Scheme.MustRegisterWithAlias(&CTFAddComponentVersion{}, CTFAddComponentVersionV1alpha1)
	Scheme.MustRegisterWithAlias(&CTFAddLocalResource{}, CTFAddLocalResourceV1alpha1)
	Scheme.MustRegisterWithAlias(&CTFGetLocalResource{}, CTFGetLocalResourceV1alpha1)
}
