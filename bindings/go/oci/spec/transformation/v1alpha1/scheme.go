package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

func init() {
	Scheme.MustRegisterWithAlias(
		&OCIGetComponentVersion{},
		runtime.NewVersionedType(OCIGetComponentVersionType, Version),
	)
	Scheme.MustRegisterWithAlias(
		&OCIAddComponentVersion{},
		runtime.NewVersionedType(OCIAddComponentVersionType, Version),
	)
	Scheme.MustRegisterWithAlias(
		&CTFGetComponentVersion{},
		runtime.NewVersionedType(CTFGetComponentVersionType, Version),
	)
	Scheme.MustRegisterWithAlias(
		&CTFAddComponentVersion{},
		runtime.NewVersionedType(CTFAddComponentVersionType, Version),
	)
}
