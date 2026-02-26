package internal

import (
	"fmt"

	ocispecv1 "github.com/opencontainers/image-spec/specs-go/v1"

	"ocm.software/open-component-model/bindings/go/oci/looseref"
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

// convertToConcreteRepo converts a runtime.Typed (which may be *runtime.Raw) to a concrete repository type.
func convertToConcreteRepo(repo runtime.Typed) (runtime.Typed, error) {
	switch r := repo.(type) {
	case *oci.Repository, *ctfv1.Repository:
		return repo, nil
	case *runtime.Raw:
		obj, err := Scheme.NewObject(r.Type)
		if err != nil {
			return nil, fmt.Errorf("cannot create object for type %s: %w", r.Type, err)
		}
		if err := Scheme.Convert(r, obj); err != nil {
			return nil, fmt.Errorf("cannot convert raw to concrete type: %w", err)
		}
		return obj, nil
	default:
		return nil, fmt.Errorf("unknown repository type %T", repo)
	}
}

func ChooseAddType(repo runtime.Typed) (runtime.Type, error) {
	concreteRepo, err := convertToConcreteRepo(repo)
	if err != nil {
		return runtime.Type{}, fmt.Errorf("converting repository spec: %w", err)
	}
	switch concreteRepo.(type) {
	case *oci.Repository:
		return ociv1alpha1.OCIAddComponentVersionV1alpha1, nil
	case *ctfv1.Repository:
		return ociv1alpha1.CTFAddComponentVersionV1alpha1, nil
	default:
		return runtime.Type{}, fmt.Errorf("unsupported repository type %T for add operation", concreteRepo)
	}
}

func ChooseGetLocalResourceType(repo runtime.Typed) (runtime.Type, error) {
	concreteRepo, err := convertToConcreteRepo(repo)
	if err != nil {
		return runtime.Type{}, fmt.Errorf("converting repository spec: %w", err)
	}
	switch concreteRepo.(type) {
	case *oci.Repository:
		return ociv1alpha1.OCIGetLocalResourceV1alpha1, nil
	case *ctfv1.Repository:
		return ociv1alpha1.CTFGetLocalResourceV1alpha1, nil
	default:
		return runtime.Type{}, fmt.Errorf("unsupported repository type %T for get local resource operation", concreteRepo)
	}
}

func ChooseAddLocalResourceType(repo runtime.Typed) (runtime.Type, error) {
	concreteRepo, err := convertToConcreteRepo(repo)
	if err != nil {
		return runtime.Type{}, fmt.Errorf("converting repository spec: %w", err)
	}
	switch concreteRepo.(type) {
	case *oci.Repository:
		return ociv1alpha1.OCIAddLocalResourceV1alpha1, nil
	case *ctfv1.Repository:
		return ociv1alpha1.CTFAddLocalResourceV1alpha1, nil
	default:
		return runtime.Type{}, fmt.Errorf("unsupported repository type %T for add local resource operation", concreteRepo)
	}
}

func ParseReferenceName(imageReference string) (string, error) {
	if imageReference == "" {
		return "", fmt.Errorf("cannot get reference name from empty image reference")
	}
	imageRef, err := looseref.ParseReference(imageReference)
	if err != nil {
		return "", fmt.Errorf("invalid OCI image reference %q: %w", imageReference, err)
	}
	if imageRef.Repository == "" {
		return "", fmt.Errorf("invalid image reference %q: repository is required", imageRef)
	}
	referenceName := imageRef.Repository
	if imageRef.Tag != "" {
		referenceName += ":" + imageRef.Tag
	}
	return referenceName, nil
}

// IsOCICompliantManifest checks if a descriptor describes a manifest that is recognizable by OCI.
// TODO(fabianburth): this is currently directly copied from
//
//	bindings/go/oci/internal/introspection/manifest.go. We accept this for now
//	as we want to rework transfer behaviour towards a config based mechanism
//	soon anyways after which we might not need this function here anymore.
func IsOCICompliantManifest(mediaType string) bool {
	switch mediaType {
	// TODO(jakobmoellerdev): currently only Image Indexes and OCI manifests are supported,
	//  but we may want to extend this down the line with additional media types such as docker manifests.
	case ocispecv1.MediaTypeImageManifest,
		ocispecv1.MediaTypeImageIndex:
		return true
	default:
		return false
	}
}
