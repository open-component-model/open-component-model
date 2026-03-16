package functions

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"k8s.io/apiserver/pkg/cel/lazy"

	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci/looseref"
	ociaccess "ocm.software/open-component-model/bindings/go/oci/spec/access"
	ociaccessv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const ToOCIFunctionName = "toOCI"

var scheme = runtime.NewScheme()

func init() {
	scheme.MustRegisterScheme(ociaccess.Scheme)
	scheme.MustRegisterScheme(v2.Scheme)
}

// ToOCI returns a CEL environment option that registers the "toOCI" function.
// This function can be called on any CEL value (string or map) and converts
// it into a map containing OCI reference components (host, registry, repository,
// reference, tag, digest).
func ToOCI() cel.EnvOption {
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
		cel.SingletonUnaryBinding(BindingToOCI),
	)
}

// BindingToOCI is the implementation of the toOCI function.
// It accepts a CEL value (string or map[string]any) representing an OCI image reference,
// parses it into host, repository, tag, and digest components, and returns a lazy map
// of those components as strings.
// If the input is:
//   - string: the entire value is treated as the reference string
//   - map[string]any with "imageReference": treated as an OCIImage access
//   - map[string]any with "globalAccess": treated as a localBlob access,
//     the "imageReference" is extracted from the nested "globalAccess" map
//   - map[string]any with "referenceName": treated as a localBlob access,
//     the "referenceName" is used as the reference string
//
// The function returns an error if parsing fails or the map is malformed.
func BindingToOCI(lhs ref.Val) ref.Val {
	var reference string

	// Determine the reference string from the input value
	switch v := lhs.Value().(type) {
	case string:
		reference = v
	case map[string]any:
		imgRef, err := extractImageReference(v)
		if err != nil {
			return types.NewErr("%s", err)
		}
		reference = imgRef
	default:
		return types.NewErr("expected string or map with key 'imageReference', 'globalAccess', or 'referenceName', got %T", lhs.Value())
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

// extractImageReference extracts an OCI image reference from a map.
// It supports both OCIImage access (with "imageReference" key) and localBlob
// access (with "globalAccess" containing an "imageReference" key, or
// "referenceName" as a fallback).
func extractImageReference(m map[string]any) (string, error) {
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

	if imgRef, err := getAccessReference(&raw); imgRef != "" {
		return imgRef, nil
	} else if err != nil {
		slog.Warn("extracting image reference from access type failed, falling back to untyped map access", "error", err)
	}

	// try old way with "imageReference" key at the top level (for backward compatibility)
	if imgRef, ok := m["imageReference"].(string); ok {
		slog.Warn("toOCI(): falling back to untyped 'imageReference' field, please use a proper OCM access type (e.g. OCIImage/v1 or localBlob/v1)",
			"imageReference", imgRef)
		return imgRef, nil
	}

	return "", fmt.Errorf("expected map with key 'imageReference', 'globalAccess.imageReference', or 'referenceName', got %v", m)
}

func getAccessReference(raw *runtime.Raw) (string, error) {
	typed, err := scheme.NewObject(raw.GetType())
	if err != nil {
		return "", fmt.Errorf("creating new object for type %s failed: %w", raw.GetType(), err)
	}

	switch typed.(type) {
	case *ociaccessv1.OCIImage:
		var ociImage ociaccessv1.OCIImage
		if err := scheme.Convert(raw, &ociImage); err != nil {
			return "", fmt.Errorf("converting to OCIImage failed: %w", err)
		}
		return ociImage.ImageReference, nil
	case *v2.LocalBlob:
		var localBlob v2.LocalBlob
		if err := scheme.Convert(raw, &localBlob); err != nil {
			return "", fmt.Errorf("converting to LocalBlob failed: %w", err)
		}

		if localBlob.GlobalAccess != nil {
			var globalOCIImage ociaccessv1.OCIImage
			if err := ociaccess.Scheme.Convert(localBlob.GlobalAccess, &globalOCIImage); err == nil && globalOCIImage.ImageReference != "" {
				return globalOCIImage.ImageReference, nil
			}
		}
	}

	return "", fmt.Errorf("no valid OCI image reference found in access type %s", typed.GetType())
}
