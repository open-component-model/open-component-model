package transfer

import (
	"context"

	"ocm.software/open-component-model/bindings/go/oci/compref"
	"ocm.software/open-component-model/bindings/go/repository/component/resolvers"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer/internal"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
)

// BuildGraphDefinition constructs a [transformv1alpha1.TransformationGraphDefinition] that
// describes how to transfer a component version from a source repository to a target repository.
//
// The returned graph definition can be executed by a [builder.Builder] (see [NewDefaultBuilder])
// to perform the actual transfer.
//
// fromSpec identifies the source component version to transfer. It must contain the component name,
// version, and a repository specification that the repoResolver can use to locate the component.
// Use [compref.Parse] to construct a Ref from a string like "ghcr.io/org/repo//ocm.software/mycomponent:1.0.0".
// Note: In the future we may decide to support passing many component versions (like all CVs from a CTF) but this
// is currently not implemented.
//
// toSpec is the target repository specification where the component version will be transferred to.
// Supported types are [oci.Repository] (OCI registry) and [ctf.Repository] (Common Transport Format archive).
// Use [compref.ParseRepository] to construct a repository spec from a string like "ghcr.io/target/repo".
//
// repoResolver is used to discover component versions during the transfer. It resolves component
// names and versions to concrete repository specifications and repositories, enabling recursive
// discovery of referenced components when [WithRecursive] is set.
//
// By default, only the component descriptor itself is transferred. Use [WithCopyMode] to also
// transfer resources, and [WithUploadType] to control how resources are stored in the target.
func BuildGraphDefinition(
	ctx context.Context,
	fromSpec *compref.Ref,
	toSpec runtime.Typed,
	repoResolver resolvers.ComponentVersionRepositoryResolver,
	opts ...Option,
) (*transformv1alpha1.TransformationGraphDefinition, error) {
	o := Options{}
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	return internal.BuildGraphDefinition(ctx, fromSpec, toSpec, repoResolver, o.Recursive, int(o.CopyMode), int(o.UploadType))
}
