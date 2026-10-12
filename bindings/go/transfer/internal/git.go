package internal

import (
	"fmt"
	"reflect"

	"cel.dev/cel-go/cel"
	"cel.dev/cel-go/common/types"
	"cel.dev/cel-go/common/types/ref"

	filev1alpha1 "ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	gitv1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
	identityv1 "ocm.software/open-component-model/bindings/go/git/spec/identity/v1"
	gitv1alpha1 "ocm.software/open-component-model/bindings/go/git/transformation/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
)

const toGitFunctionName = "toGit"

// gitOrigin is the origin of a Git resource the Git uploader replicates: the path of its
// repository, without scheme, host and port, and the ref it was selected by. Like the
// referenceName of an OCI image copied as a local blob, it is relative to the server, so it
// can be placed below another one.
type gitOrigin struct {
	Repository string
	Ref        string
}

// gitOriginOf returns the origin of a Git access.
func gitOriginOf(access *gitv1.Git) (gitOrigin, error) {
	id, err := identityv1.IdentityFromURL(access.Repository)
	if err != nil {
		return gitOrigin{}, fmt.Errorf("cannot address git repository: %w", err)
	}
	return gitOrigin{Repository: id[runtime.IdentityAttributePath], Ref: access.Ref}, nil
}

// toGitFunction declares <access>.toGit(): the origin of a Git access as a map with
// repository (the repository path) and ref, like toOCI() for OCI image accesses.
func toGitFunction() cel.EnvOption {
	return cel.Function(toGitFunctionName,
		cel.MemberOverload("toGit_dyn_member", []*cel.Type{cel.DynType}, cel.MapType(cel.StringType, cel.StringType),
			cel.UnaryBinding(bindToGit),
		),
	)
}

func bindToGit(val ref.Val) ref.Val {
	native, err := val.ConvertToNative(reflect.TypeFor[map[string]any]())
	if err != nil {
		return types.NewErr("%s() expects an access, got %T", toGitFunctionName, val.Value())
	}
	access := native.(map[string]any)
	typ, _ := access["type"].(string)
	accessType, err := runtime.TypeFromString(typ)
	if err != nil {
		return types.NewErr("%s() expects an access with a type: %v", toGitFunctionName, err)
	}

	var origin gitOrigin
	switch {
	case accessTypeIs(accessType, runtime.NewUnversionedType(gitv1.Type)):
		repository, _ := access["repository"].(string)
		reference, _ := access["ref"].(string)
		if origin, err = gitOriginOf(&gitv1.Git{Repository: repository, Ref: reference}); err != nil {
			return types.NewErr("%s(): %v", toGitFunctionName, err)
		}
	default:
		return types.NewErr("%s() expects a Git access, got %s", toGitFunctionName, typ)
	}
	return types.DefaultTypeAdapter.NativeToValue(map[string]string{
		"repository": origin.Repository,
		"ref":        origin.Ref,
	})
}

// processGit emits the transformation nodes for a Git resource copied by value. The
// snapshot at the pinned commit is downloaded (GetGitResource), then embedded as a local
// blob (AddLocalResource) of [transferv1alpha1.GitLocalBlobMediaType] so it is recognizable
// as a Git archive. The origin is not recorded and no referenceName is set: pushing such a
// local blob with the Git uploader needs an explicit repository and ref.
func processGit(resource descriptorv2.Resource, access *gitv1.Git, id string, val *discoveryValue, tgd *transformv1alpha1.TransformationGraphDefinition, toSpec runtime.Typed, resourceTransformIDs map[int]string, i int) error {
	resourceID := identityToTransformationID(resource.ToIdentity())
	getResourceID := fmt.Sprintf("%sGet%s", id, resourceID)
	addResourceID := fmt.Sprintf("%sAdd%s", id, resourceID)

	getTransform, err := getGitTransformation(resource, access, getResourceID, val)
	if err != nil {
		return err
	}

	addResourceTransform, err := uploadAsLocalResource(toSpec, val.Descriptor.Component.Name, val.Descriptor.Component.Version, addResourceID, getResourceID, "", addLabel(&val.Descriptor.Component, resource.Name, "LocalBlob", toSpec))
	if err != nil {
		return fmt.Errorf("failed to create local resource upload transformation: %w", err)
	}
	// Mark the local blob as a Git archive so an explicit git uploader match can select it.
	addResourceTransform.Spec.Data["file"] = map[string]any{
		"type":      runtime.NewVersionedType(filev1alpha1.FileType, filev1alpha1.Version).String(),
		"uri":       fmt.Sprintf("${%s.output.file.uri}", getResourceID),
		"mediaType": transferv1alpha1.GitLocalBlobMediaType,
	}
	tgd.Transformations = append(tgd.Transformations, getTransform, addResourceTransform)
	resourceTransformIDs[i] = addResourceID

	return nil
}

// getGitTransformation returns the GetGitResource transformation that downloads the
// archive of resource at its pinned commit. Transfer by value requires a pinned commit,
// because a ref can move and the archive would no longer match the resource digest.
func getGitTransformation(resource descriptorv2.Resource, access *gitv1.Git, getResourceID string, val *discoveryValue) (transformv1alpha1.GenericTransformation, error) {
	if access.Commit == "" {
		return transformv1alpha1.GenericTransformation{}, fmt.Errorf("git resource %q has no pinned commit: pin ref %q to a commit before transferring it by value", resource.Name, access.Ref)
	}
	unstructured, err := runtime.UnstructuredFromMixedData(map[string]any{
		"resource": resource,
	})
	if err != nil {
		return transformv1alpha1.GenericTransformation{}, fmt.Errorf("cannot create unstructured spec for GetGitResource transformation: %w", err)
	}
	return transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type:  gitv1alpha1.GetGitResourceV1alpha1,
			ID:    getResourceID,
			Label: getLabel(&val.Descriptor.Component, resource.Name),
		},
		Spec: unstructured,
	}, nil
}
