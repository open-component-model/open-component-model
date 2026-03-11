package functions

import (
	"fmt"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"k8s.io/apiserver/pkg/cel/lazy"

	"ocm.software/open-component-model/bindings/go/oci/looseref"
)

const ToOCIFunctionName = "toOCI"

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
		return types.NewErr("expected string or map with key 'imageReference', got %T", lhs.Value())
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
	// Direct imageReference (OCIImage access)
	if imgRef, ok := m["imageReference"]; ok {
		imRefStr, ok := imgRef.(string)
		if !ok {
			return "", fmt.Errorf("expected 'imageReference' to be a string, got %T", imgRef)
		}
		return imRefStr, nil
	}

	// localBlob access: prefer globalAccess.imageReference, fall back to referenceName
	if ga, ok := m["globalAccess"]; ok {
		gaMap, ok := ga.(map[string]any)
		if !ok {
			return "", fmt.Errorf("expected 'globalAccess' to be a map, got %T", ga)
		}
		if imgRef, ok := gaMap["imageReference"]; ok {
			imgRefStr, ok := imgRef.(string)
			if !ok {
				return "", fmt.Errorf("expected 'globalAccess.imageReference' to be a string, got %T", imgRef)
			}
			return imgRefStr, nil
		}
	}

	// localBlob access: fall back to referenceName
	if refName, ok := m["referenceName"]; ok {
		imgRefStref, ok := refName.(string)
		if !ok {
			return "", fmt.Errorf("expected 'referenceName' to be a string, got %T", refName)
		}
		if imgRefStref != "" {
			return imgRefStref, nil
		}
	}

	return "", fmt.Errorf("expected map with key 'imageReference', 'globalAccess.imageReference', or 'referenceName', got %v", m)
}
