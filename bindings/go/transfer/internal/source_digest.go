package internal

import (
	"reflect"

	"cel.dev/cel-go/cel"
	"cel.dev/cel-go/common/types"
	"cel.dev/cel-go/common/types/ref"
)

// withSourceDigestFunctionName is the CEL member function that gives a target resource the
// digest of its source resource.
const withSourceDigestFunctionName = "withSourceDigest"

// withSourceDigestFunction declares <resource>.withSourceDigest(<source resource>): the
// resource with the digest of the source resource, or without a digest when the source has
// none.
//
// A transfer copies the digest of a resource exactly as recorded, even a missing one that
// the target repository computes while storing the content. A parent's reference digest may
// already cover the child descriptor without that digest, so adding it during transfer
// breaks the parent's reference. Digests are computed only while a component version is
// added. A partial digest never reaches this point: the target repository rejects it.
func withSourceDigestFunction() cel.EnvOption {
	return cel.Function(withSourceDigestFunctionName,
		cel.MemberOverload("withSourceDigest_dyn_dyn", []*cel.Type{cel.DynType, cel.DynType}, cel.DynType,
			cel.BinaryBinding(bindWithSourceDigest)),
	)
}

func bindWithSourceDigest(target, source ref.Val) ref.Val {
	mapType := reflect.TypeFor[map[string]any]()
	resource, err := target.ConvertToNative(mapType)
	if err != nil {
		return types.NewErr("%s(): target resource is not an object: %s", withSourceDigestFunctionName, err)
	}
	src, err := source.ConvertToNative(mapType)
	if err != nil {
		return types.NewErr("%s(): source resource is not an object: %s", withSourceDigestFunctionName, err)
	}
	result := resource.(map[string]any)
	if digest, ok := src.(map[string]any)["digest"]; ok && digest != nil {
		result["digest"] = digest
	} else {
		delete(result, "digest")
	}
	return types.DefaultTypeAdapter.NativeToValue(result)
}
