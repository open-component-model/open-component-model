package transfer

import (
	"context"

	"ocm.software/open-component-model/bindings/go/oci/compref"
	"ocm.software/open-component-model/bindings/go/repository/component/resolvers"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer/internal"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
)

// BuildGraphDefinition constructs a TransformationGraphDefinition for transferring
// a component version (and optionally its resources) from source to target.
func BuildGraphDefinition(
	ctx context.Context,
	fromSpec *compref.Ref,
	toSpec runtime.Typed,
	repoResolver resolvers.ComponentVersionRepositoryResolver,
	opts ...Option,
) (*transformv1alpha1.TransformationGraphDefinition, error) {
	o := Options{}
	for _, opt := range opts {
		opt(&o)
	}
	return internal.BuildGraphDefinition(ctx, fromSpec, toSpec, repoResolver, o.Recursive, int(o.CopyMode), int(o.UploadType))
}
