package internal

import (
	"fmt"

	"ocm.software/open-component-model/bindings/go/oci/looseref"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
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

// validateUploadType checks that the given upload type is compatible with the target repository.
// Uploading as OCI artifact to a CTF archive is not allowed because CTF archives have no
// registry URL to resolve against.
func validateUploadType(uploadType UploadType, toSpec runtime.Typed) error {
	if uploadType == UploadAsOciArtifact {
		if _, isCTF := toSpec.(*ctfv1.Repository); isCTF {
			return fmt.Errorf("cannot upload as OCI artifact to a CTF archive: CTF archives have no registry URL to resolve against, use --upload-as localBlob or omit the flag to use the default behavior")
		}
	}
	return nil
}

// shouldUploadAsOCIArtifact determines whether an OCI artifact resource should be uploaded
// as an OCI artifact reference or as a local blob based on the upload type and target repository.
//
// When uploadType is UploadAsDefault, the decision is based on the target repository type:
//   - OCI registry targets (*oci.Repository) → OCI artifact (the registry can host the image)
//   - CTF targets (*ctfv1.Repository) → local blob (CTF archives have no registry URL to resolve against)
//
// Explicit upload types (UploadAsOciArtifact, UploadAsLocalBlob) always take precedence.
func shouldUploadAsOCIArtifact(uploadType UploadType, toSpec runtime.Typed) bool {
	switch uploadType {
	case UploadAsOciArtifact:
		return true
	case UploadAsLocalBlob:
		return false
	default: // UploadAsDefault
		// Default behavior: upload as OCI artifact only when targeting an OCI registry.
		// CTF targets should receive local blobs since they don't have a registry URL.
		_, isOCI := toSpec.(*oci.Repository)
		return isOCI
	}
}

func GetReferenceName(ociAccess ociv1.OCIImage) (string, error) {
	if ociAccess.ImageReference == "" {
		return "", fmt.Errorf("cannot get reference name from empty image reference")
	}
	imageRef, err := looseref.ParseReference(ociAccess.ImageReference)
	if err != nil {
		return "", fmt.Errorf("invalid OCI image reference %q: %w", ociAccess.ImageReference, err)
	}
	if imageRef.Repository == "" {
		return "", fmt.Errorf("invalid image reference %q: repository is required", ociAccess.ImageReference)
	}
	referenceName := imageRef.Repository
	if imageRef.Tag != "" {
		referenceName += ":" + imageRef.Tag
	}
	return referenceName, nil
}
