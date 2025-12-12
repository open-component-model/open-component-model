package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

var (
	OCIGetComponentVersionV1alpha1 = runtime.NewVersionedType(OCIGetComponentVersionType, Version)
	OCIAddComponentVersionV1alpha1 = runtime.NewVersionedType(OCIAddComponentVersionType, Version)
	CTFGetComponentVersionV1alpha1 = runtime.NewVersionedType(CTFGetComponentVersionType, Version)
	CTFAddComponentVersionV1alpha1 = runtime.NewVersionedType(CTFAddComponentVersionType, Version)
)

func init() {
	Scheme.MustRegisterWithAlias(&OCIGetComponentVersion{}, OCIGetComponentVersionV1alpha1)
	Scheme.MustRegisterWithAlias(&OCIAddComponentVersion{}, OCIAddComponentVersionV1alpha1)
	Scheme.MustRegisterWithAlias(&CTFGetComponentVersion{}, CTFGetComponentVersionV1alpha1)
	Scheme.MustRegisterWithAlias(&CTFAddComponentVersion{}, CTFAddComponentVersionV1alpha1)
}
