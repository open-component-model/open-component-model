package internal

import (
	"fmt"

	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	ociv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func AsUnstructured(typed runtime.Typed) *runtime.Unstructured {
	var raw runtime.Raw
	if err := runtime.NewScheme(runtime.WithAllowUnknown()).Convert(typed, &raw); err != nil {
		panic(fmt.Sprintf("cannot convert to raw: %v", err))
	}
	var unstructured runtime.Unstructured
	if err := runtime.NewScheme(runtime.WithAllowUnknown()).Convert(&raw, &unstructured); err != nil {
		panic(fmt.Sprintf("cannot convert to unstructured: %v", err))
	}
	return &unstructured
}

func ChooseGetType(repo runtime.Typed) runtime.Type {
	switch repo.(type) {
	case *oci.Repository:
		return ociv1alpha1.OCIGetComponentVersionV1alpha1
	case *ctfv1.Repository:
		return ociv1alpha1.CTFGetComponentVersionV1alpha1
	default:
		panic(fmt.Sprintf("unknown repository type %T", repo))
	}
}

func ChooseAddType(repo runtime.Typed) runtime.Type {
	switch repo.(type) {
	case *oci.Repository:
		return ociv1alpha1.OCIAddComponentVersionV1alpha1
	case *ctfv1.Repository:
		return ociv1alpha1.CTFAddComponentVersionV1alpha1
	default:
		panic(fmt.Sprintf("unknown repository type %T", repo))
	}
}
