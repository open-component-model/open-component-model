package internal

import (
	"fmt"
	"net/url"

	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
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

func ChooseGetLocalResourceType(repo runtime.Typed) runtime.Type {
	switch repo.(type) {
	case *oci.Repository:
		return ociv1alpha1.OCIGetLocalResourceV1alpha1
	case *ctfv1.Repository:
		return ociv1alpha1.CTFGetLocalResourceV1alpha1
	default:
		panic(fmt.Sprintf("unknown repository type %T", repo))
	}
}

func ChooseAddLocalResourceType(repo runtime.Typed) runtime.Type {
	switch repo.(type) {
	case *oci.Repository:
		return ociv1alpha1.OCIAddLocalResourceV1alpha1
	case *ctfv1.Repository:
		return ociv1alpha1.CTFAddLocalResourceV1alpha1
	default:
		panic(fmt.Sprintf("unknown repository type %T", repo))
	}
}

func isLocalRelation(resource v2.Resource) bool {
	return resource.Relation == v2.LocalRelation
}

func GetImageReference(ociImage v1.OCIImage) (string, error) {
	var referenceName string
	if ociImage.ImageReference != "" {
		u, err := url.Parse(ociImage.ImageReference)
		if err != nil {
			return "", fmt.Errorf("invalid OCI image reference: %s", ociImage.ImageReference)
		}
		referenceName = u.Path
	}
	return referenceName, nil
}
