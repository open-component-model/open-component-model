package internal

import (
	"context"
	"reflect"
	"sync"

	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/cli/internal/reference/compref"
)

// processor is implemented by types that can process a resource of a specific access type and add the necessary transformations to the graph definition.
type processor interface {
	Process(ctx context.Context, resource descriptorv2.Resource, id string, ref *compref.Ref, tgd *transformv1alpha1.TransformationGraphDefinition, toSpec runtime.Typed, resourceTransformIDs map[int]string, i int, uploadAsOCIArtifact bool) error
}

// ociUploadSupported is implemented by processors that support optionally uploading the resource as an OCI artifact.
// This allows the decision whether to upload as OCI artifact to be made based on the resource and target repository characteristics.
type ociUploadSupported interface {
	ShouldUploadAsOCIArtifact(ctx context.Context, resource descriptorv2.Resource, toSpec runtime.Typed, access runtime.Typed, uploadType UploadType) (bool, error)
}

// processors for different resource access types. The key is the value type of the access object in the descriptor.
var processors = map[reflect.Type]processor{}

// ociUploaders for different resource access types. The key is the value type of the access object in the descriptor.
// This is an optional mapping that processors can implement if they support uploading the resource as an OCI artifact.
var ociUploaders = map[reflect.Type]ociUploadSupported{}

var mutex sync.Mutex

// registerProcessor registers a processor and optional ociUploadSupported for the given access type prototype.
// The accessPrototype should be a runtime.Typed, e.g. &helmv1.Helm{}.
func registerProcessor(accessPrototype runtime.Typed, proc processor) {
	mutex.Lock()
	defer mutex.Unlock()

	// we want the values here, not the pointers e.g. helmv1.Helm instead of *helmv1.Helm
	typ := derefType(reflect.TypeOf(accessPrototype))
	processors[typ] = proc
	if uploader, ok := proc.(ociUploadSupported); ok {
		ociUploaders[typ] = uploader
	}
}

// derefType returns the element type if typ is a pointer, otherwise returns typ as-is.
// This is needed because Scheme.NewObject returns pointers but the maps are keyed by value types.
func derefType(typ reflect.Type) reflect.Type {
	if typ.Kind() == reflect.Pointer {
		return typ.Elem()
	}
	return typ
}

func lookupOCIUploadSupported(access runtime.Typed) (ociUploadSupported, bool) {
	// we want the values here, not the pointers e.g. helmv1.Helm instead of *helmv1.Helm
	typ := derefType(reflect.TypeOf(access))
	p, ok := ociUploaders[typ]
	return p, ok
}

func lookupProcessor(access runtime.Typed) (processor, bool) {
	// we want the values here, not the pointers e.g. helmv1.Helm instead of *helmv1.Helm
	typ := derefType(reflect.TypeOf(access))
	p, ok := processors[typ]
	return p, ok
}
