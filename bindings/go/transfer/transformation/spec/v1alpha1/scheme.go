package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

var (
	ArtifactoryUploadV1alpha1 = runtime.NewVersionedType(ArtifactoryUploadType, Version)
	NexusUploadV1alpha1       = runtime.NewVersionedType(NexusUploadType, Version)
)

func init() {
	Scheme.MustRegisterWithAlias(&ArtifactoryUpload{}, ArtifactoryUploadV1alpha1)
	Scheme.MustRegisterWithAlias(&NexusUpload{}, NexusUploadV1alpha1)
}
