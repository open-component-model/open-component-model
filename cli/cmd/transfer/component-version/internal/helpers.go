package internal

import (
	"fmt"

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

func isLocalBlobAccess(access *runtime.Raw) bool {
	if access == nil {
		return false
	}
	return access.Type.Name == v2.LocalBlobAccessType
}

// isOCIArtifactAccess checks if the given access specification is for an OCI artifact.
// It checks for various OCI artifact type names including legacy variants.
func isOCIArtifactAccess(access *runtime.Raw) bool {
	if access == nil {
		return false
	}
	return access.Type.Name == v1.LegacyType || access.Type.Name == v1.LegacyType2 || access.Type.Name == v1.LegacyType3
}

func isLocalRelation(resource v2.Resource) bool {
	return resource.Relation == v2.LocalRelation
}

// generateTargetImageReference generates the target OCI image reference for a resource.
// Format: {baseUrl}/{subPath}/{resourceName}:{resourceVersion}
// For CTF repositories, returns empty string (artifacts are converted to local blobs).
func generateTargetImageReference(repo runtime.Typed, resourceName, resourceVersion string) string {
	switch r := repo.(type) {
	case *oci.Repository:
		// Build OCI image reference from repository base URL and subpath
		baseURL := r.BaseUrl
		if r.SubPath != "" {
			baseURL = fmt.Sprintf("%s/%s", r.BaseUrl, r.SubPath)
		}
		return fmt.Sprintf("%s/%s:%s", baseURL, resourceName, resourceVersion)
	case *ctfv1.Repository:
		// CTF: artifacts will be converted to local blobs, no image reference needed
		return ""
	default:
		panic(fmt.Sprintf("unknown repository type %T", repo))
	}
}
