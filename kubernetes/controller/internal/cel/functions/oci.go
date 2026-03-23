package functions

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"k8s.io/apiserver/pkg/cel/lazy"

	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci/looseref"
	ociaccess "ocm.software/open-component-model/bindings/go/oci/spec/access"
	ociaccessv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
)

const ToOCIFunctionName = "toOCI"

var scheme = runtime.NewScheme()

func init() {
	scheme.MustRegisterScheme(repository.Scheme)
	scheme.MustRegisterScheme(ociaccess.Scheme)
	scheme.MustRegisterScheme(v2.Scheme)
}

// ToOCI returns a CEL environment option that registers the "toOCI" function.
// This function can be called on any CEL value (string or map) and converts
// it into a map containing OCI reference components (host, registry, repository,
// reference, tag, digest).
func ToOCI(component *v1alpha1.ComponentInfo) cel.EnvOption {
	return cel.Function(
		ToOCIFunctionName,
		// Member overload: allow invoking as <value>.toOCI()
		cel.MemberOverload(
			"toOCI_dyn_member",
			[]*cel.Type{cel.DynType},
			// Return type: map<string, string>
			types.NewMapType(types.StringType, types.StringType),
		),
		// Standalone overload: allow calling toOCI(<value>)
		cel.Overload(
			"toOCI_dyn",
			[]*cel.Type{cel.DynType},
			types.NewMapType(types.StringType, types.StringType),
		),
		// Bind the overload to the Go implementation
		cel.SingletonUnaryBinding(BindingToOCI(component)),
	)
}

// BindingToOCI is the implementation of the toOCI function.
// It accepts a CEL value (string or map[string]any) representing an OCI image reference,
// parses it into host, repository, tag, and digest components, and returns a lazy map
// of those components as strings.
// If the input is:
//   - string: the entire value is treated as the reference string
//   - map[string]any with "imageReference": treated as an OCIImage access (OCIImage/v1).
//     If the access cannot be fully parsed the imageReference is returned directly as a fallback
//   - map[string]any with "localReference": treated as a localBlob access (localBlob/v1),
//     the full OCI reference is constructed from the component's repository spec
//
// v1alpha1.ComponentInfo is used to build the imageReference when the access is localBlob.
// It takes the component name, the repository spec and the localBlob's LocalReference to construct the full reference to the OCI registry.
// The function returns an error if parsing fails or the map is malformed.
func BindingToOCI(component *v1alpha1.ComponentInfo) func(lhs ref.Val) ref.Val {
	return func(lhs ref.Val) ref.Val {
		var reference string

		// Determine the reference string from the input value
		switch v := lhs.Value().(type) {
		case string:
			reference = v
		case map[string]any:
			imgRef, err := extractImageReference(v, component)
			if err != nil {
				return types.NewErr("%s", err)
			}
			reference = imgRef
		default:
			return types.NewErr("expected string or map with OCIImage or localBlob access, got %T", lhs.Value())
		}

		// Parse the OCI reference using the oci.ParseRef helper because if a reference consists of a tag and a digest,
		// we need to store both of them. Additionally, consuming resources, as a HelmRelease or OCIRepository, might need
		// the tag, the digest, or both of them. Thus, we have to offer some flexibility here.
		r, err := looseref.ParseReference(reference)
		if err != nil {
			return types.WrapErr(err)
		}

		// Extract optional tag and digest values
		var tag, digest string

		// Check for digest and ignore error (validation error indicates no digest present)
		if refDigest, err := r.Digest(); err == nil {
			digest = refDigest.String()
		}

		if r.Tag != "" {
			tag = r.Tag
		}

		// Construct a lazy map to defer value computation until accessed
		mv := lazy.NewMapValue(types.StringType)

		// host and registry are the same value (OCI spec)
		mv.Append("host", func(*lazy.MapValue) ref.Val {
			return types.String(r.Host())
		})
		mv.Append("registry", func(*lazy.MapValue) ref.Val {
			return types.String(r.Host())
		})

		// repository: trim any leading slash
		mv.Append("repository", func(*lazy.MapValue) ref.Val {
			return types.String(strings.TrimLeft(r.Repository, "/"))
		})

		// reference: either "tag@digest", tag, or digest
		mv.Append("reference", func(*lazy.MapValue) ref.Val {
			var refStr string
			switch {
			case r.Tag != "" && digest != "":
				refStr = fmt.Sprintf("%s@%s", r.Tag, digest)
			case r.Tag != "":
				refStr = r.Tag
			case digest != "":
				refStr = digest
			}

			return types.String(refStr)
		})

		// digest and tag as separate entries (empty string if missing)
		mv.Append("digest", func(*lazy.MapValue) ref.Val {
			return types.String(digest)
		})
		mv.Append("tag", func(*lazy.MapValue) ref.Val {
			return types.String(tag)
		})

		return mv
	}
}

// extractImageReference extracts an OCI image reference from a map.
// It supports OCIImage access and localBlob access).
// For backward compatibility, a plain "imageReference" key at the top level is also accepted.
func extractImageReference(m map[string]any, component *v1alpha1.ComponentInfo) (string, error) {
	unstructured, err := runtime.UnstructuredFromMixedData(m)
	if err != nil {
		return "", fmt.Errorf("converting map to unstructured failed: %w", err)
	}

	// runtime.Scheme.Convert does not support runtime.Unstructured as a conversion source.
	// We need to convert it to runtime.Raw first.
	// TODO(matthiasbruns) https://github.com/open-component-model/ocm-project/issues/944
	var raw runtime.Raw
	if err := runtime.NewScheme(runtime.WithAllowUnknown()).Convert(unstructured, &raw); err != nil {
		return "", fmt.Errorf("converting unstructured to raw failed: %w", err)
	}

	if imgRef, err := getAccessReference(&raw, component); imgRef != "" {
		return imgRef, nil
	} else if err != nil {
		slog.Warn("extracting image reference from access type failed, falling back to untyped map access", "error", err)
	}

	// try old way with "imageReference" key at the top level (for backward compatibility)
	// TODO(matthiasnbruns): we will drop support for this completely - https://github.com/open-component-model/ocm-project/issues/960
	if imgRef, ok := m["imageReference"].(string); ok {
		slog.Warn(
			"toOCI(): falling back to untyped 'imageReference' field, please use a proper OCM access type (e.g. OCIImage/v1 or localBlob/v1). "+
				"This feature is deprecated and will be removed in a future release. "+
				"You can track the progress here https://github.com/open-component-model/ocm-project/issues/960",
			"imageReference", imgRef)
		return imgRef, nil
	}

	return "", fmt.Errorf("expected map with OCIImage access (imageReference) or localBlob access (localReference) or imageReference field")
}

// getAccessReference extracts an OCI image reference from a typed access spec.
// For OCIImage access, it returns the imageReference field directly.
// For localBlob access, it decodes the component's repository spec to obtain the
// OCI registry base URL and subPath, then constructs the full reference as:
//
//	baseUrl/subPath/component-descriptors/componentName@localReference
//
// Returns an error if the access type is not recognized or conversion fails.
func getAccessReference(raw *runtime.Raw, component *v1alpha1.ComponentInfo) (string, error) {
	typed, err := scheme.NewObject(raw.GetType())
	if err != nil {
		return "", fmt.Errorf("creating new object for type %s failed: %w", raw.GetType(), err)
	}

	if err := scheme.Convert(raw, typed); err != nil {
		return "", fmt.Errorf("converting raw to typed failed: %w", err)
	}

	switch v := typed.(type) {
	case *ociaccessv1.OCIImage:
		return v.ImageReference, nil
	case *v2.LocalBlob:
		if component == nil {
			return "", fmt.Errorf("component info is nil but required to build the imageRef for localBlob")
		}

		var ociRepo oci.Repository
		if err := repository.Scheme.Decode(
			bytes.NewReader(component.RepositorySpec.Raw), &ociRepo); err != nil {
			return "", fmt.Errorf("decoding repository spec failed: %w", err)
		}

		// now that we have the ociRepo, we can build the full url
		path, err := buildImageReference(&ociRepo, v, component)
		if err != nil {
			return "", fmt.Errorf("building image reference failed: %w", err)
		}
		return path, nil
	}

	return "", fmt.Errorf("no valid image reference found in access type %s", typed.GetType())
}

// buildImageReference constructs a full OCI reference for a localBlob access by joining
// the repository's baseUrl, subPath, "component-descriptors", the component name,
// and appending the localReference digest (e.g. "https://ghcr.io/org/component-descriptors/my-component@sha256:...").
func buildImageReference(ociRepo *oci.Repository, localBlob *v2.LocalBlob, component *v1alpha1.ComponentInfo) (string, error) {
	if ociRepo.BaseUrl == "" {
		return "", fmt.Errorf("oci repository url is empty")
	}
	if localBlob.LocalReference == "" {
		return "", fmt.Errorf("local blob reference is empty")
	}
	if component == nil {
		return "", fmt.Errorf("component info is nil but required to build the imageRef for localBlob")
	}
	path, err := url.JoinPath(ociRepo.BaseUrl, ociRepo.SubPath, "component-descriptors", component.Component)
	if err != nil {
		return "", fmt.Errorf("could not build path for oci image reference: %w", err)
	}
	path = fmt.Sprintf("%s@%s", path, localBlob.LocalReference)
	return path, nil
}
