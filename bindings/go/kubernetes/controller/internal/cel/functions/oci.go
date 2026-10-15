// Package functions provides controller-specific helpers that extend the shared
// OCI CEL functions (package ocm.software/open-component-model/bindings/go/oci/cel/functions)
// with resolvers that depend on the Kubernetes controller's v1alpha1 API types.
package functions

import (
	"bytes"
	"fmt"
	"net/url"

	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	ocifunctions "ocm.software/open-component-model/bindings/go/oci/cel/functions"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var scheme = runtime.NewScheme()

func init() {
	scheme.MustRegisterScheme(v2.Scheme)
}

// LocalBlobResolver returns a [ocifunctions.ReferenceResolver] that resolves localBlob
// accesses into OCI references using the component's repository spec. The reference is
// constructed as baseUrl/subPath/component-descriptors/<component>@<localReference>.
//
// Access types other than localBlob/v1 are reported as ok=false so the next resolver
// (or the deprecation fallback) is consulted. A nil component causes a hard error when
// a localBlob access is encountered.
func LocalBlobResolver(component *v1alpha1.ComponentInfo) ocifunctions.ReferenceResolver {
	return func(access *runtime.Raw) (ocifunctions.Reference, bool, error) {
		typed, err := scheme.NewObject(access.GetType())
		if err != nil {
			// Not a type this resolver knows about → skip.
			return ocifunctions.Reference{}, false, nil
		}

		if err := scheme.Convert(access, typed); err != nil {
			return ocifunctions.Reference{}, false, fmt.Errorf("converting raw to typed failed: %w", err)
		}

		localBlob, ok := typed.(*v2.LocalBlob)
		if !ok {
			return ocifunctions.Reference{}, false, nil
		}

		if component == nil {
			return ocifunctions.Reference{}, false, fmt.Errorf("component info is nil but required to build the imageRef for localBlob")
		}

		imgRef, err := buildImageReference(localBlob, component)
		if err != nil {
			return ocifunctions.Reference{}, false, fmt.Errorf("building image reference failed: %w", err)
		}

		ref, err := ocifunctions.ParseReference(imgRef)
		if err != nil {
			return ocifunctions.Reference{}, false, err
		}
		return ref, true, nil
	}
}

// buildImageReference constructs a full OCI reference for a localBlob access by joining
// the repository's baseUrl, subPath, "component-descriptors", the component name,
// and appending the localReference digest (e.g. "ghcr.io/org/component-descriptors/my-component@sha256:...").
func buildImageReference(localBlob *v2.LocalBlob, component *v1alpha1.ComponentInfo) (string, error) {
	if localBlob.LocalReference == "" {
		return "", fmt.Errorf("local blob reference is empty")
	}
	if component == nil {
		return "", fmt.Errorf("component info is nil but required to build the imageRef for localBlob")
	}

	var ociRepo oci.Repository
	if err := repository.Scheme.Decode(
		bytes.NewReader(component.RepositorySpec.Raw), &ociRepo); err != nil {
		return "", fmt.Errorf("decoding repository spec failed: %w", err)
	}

	if ociRepo.BaseUrl == "" {
		return "", fmt.Errorf("oci repository url is empty")
	}

	path, err := url.JoinPath(ociRepo.BaseUrl, ociRepo.SubPath, "component-descriptors", component.Component)
	if err != nil {
		return "", fmt.Errorf("could not build path for oci image reference: %w", err)
	}
	path = fmt.Sprintf("%s@%s", path, localBlob.LocalReference)
	return path, nil
}
