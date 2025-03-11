package extensions

import "ocm.software/open-component-model/bindings/go/runtime"

var Scheme = runtime.NewScheme()

func init() {
	MustAddToScheme(Scheme)
}

func MustAddToScheme(scheme *runtime.Scheme) {
	obj := &LocalBlob{}
	scheme.MustRegister(obj, "v1")
	scheme.MustRegister(obj, "")
	scheme.MustRegisterWithAlias(obj, LocalBlobAccessType, LocalBlobAccessTypeV1)
	upload := &LocalBlobUpload{}
	scheme.MustRegister(upload, "v1")
	scheme.MustRegister(upload, "")
	scheme.MustRegisterWithAlias(upload, LocalBlobUploadType)
}
