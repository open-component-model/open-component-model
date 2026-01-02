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
//   - map[string]any: must contain an "imageReference" key with a string value
//
// The function returns an error if parsing fails or the map is malformed.
func BindingToOCI(lhs ref.Val) ref.Val {
	var reference string

	// Determine the reference string from the input value
	switch v := lhs.Value().(type) {
	case string:
		reference = v
	case map[string]any:
		// Expect a key "imageReference"
		fromMap, ok := v["imageReference"]
		if !ok {
			return types.NewErr("expected map with key 'imageReference', got %v", v)
		}
		// Ensure the value is a string
		reference, ok = fromMap.(string)
		if !ok {
			return types.NewErr("expected map with key 'imageReference' to be a string, got %T", fromMap)
		}
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
