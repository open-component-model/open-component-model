package internal

import (
	"fmt"
	"reflect"

	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
)

// uploaderCall carries what an uploader handler needs for resource i of one component/target.
type uploaderCall struct {
	resource             descriptorv2.Resource
	access               runtime.Typed
	aliases              map[string]string
	env                  *uploaderEnv
	baseID, id           string
	val                  *discoveryValue
	tgd                  *transformv1alpha1.TransformationGraphDefinition
	toSpec               runtime.Typed
	resourceTransformIDs map[int]string
	i                    int
}

// uploaderHandler emits the transformations for a resource selected by u and returns
// the CEL spec-field expressions of file buffers to clean up.
type uploaderHandler func(u transferv1alpha1.UploaderConfig, c uploaderCall) ([]string, error)

// uploaderHandlers maps each uploader config Go type to its handler. Every type
// registered in transferv1alpha1.Scheme that implements UploaderConfig needs an entry.
var uploaderHandlers = map[reflect.Type]uploaderHandler{
	reflect.TypeFor[*transferv1alpha1.HTTPUploaderConfig](): func(u transferv1alpha1.UploaderConfig, c uploaderCall) ([]string, error) {
		return nil, processHTTPUploader(c.resource, u.(*transferv1alpha1.HTTPUploaderConfig), c.baseID, c.id, c.val, c.tgd, c.resourceTransformIDs, c.i)
	},
	reflect.TypeFor[*transferv1alpha1.OCIUploaderConfig](): func(u transferv1alpha1.UploaderConfig, c uploaderCall) ([]string, error) {
		return processOCIUploader(c.resource, c.access, u.(*transferv1alpha1.OCIUploaderConfig), c.aliases, c.env, c.id, c.val, c.tgd, c.toSpec, c.resourceTransformIDs, c.i)
	},
	reflect.TypeFor[*transferv1alpha1.LocalBlobUploaderConfig](): func(_ transferv1alpha1.UploaderConfig, c uploaderCall) ([]string, error) {
		return processResource(c.resource, c.access, c.id, c.val, c.tgd, c.toSpec, c.resourceTransformIDs, c.i)
	},
	// No transformation: buildDescriptorSpec keeps the environment resource.
	reflect.TypeFor[*transferv1alpha1.ReferenceUploaderConfig](): func(_ transferv1alpha1.UploaderConfig, c uploaderCall) ([]string, error) {
		if descriptorv2.IsLocalBlob(c.access) {
			return nil, fmt.Errorf("local blobs cannot be kept by reference (adjust match)")
		}
		return nil, nil
	},
}
