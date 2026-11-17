package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

var Scheme = runtime.NewScheme()

var (
	ArtifactoryUploadV1alpha1 = runtime.NewVersionedType(ArtifactoryUploadType, Version)
	NexusUploadV1alpha1       = runtime.NewVersionedType(NexusUploadType, Version)

	TransferWgetToOCILocalBlobV1alpha1 = runtime.NewVersionedType(TransferWgetToOCILocalBlobType, Version)
	TransferWgetToCTFLocalBlobV1alpha1 = runtime.NewVersionedType(TransferWgetToCTFLocalBlobType, Version)
	TransferS3ToOCILocalBlobV1alpha1   = runtime.NewVersionedType(TransferS3ToOCILocalBlobType, Version)
	TransferS3ToCTFLocalBlobV1alpha1   = runtime.NewVersionedType(TransferS3ToCTFLocalBlobType, Version)
)

func init() {
	Scheme.MustRegisterWithAlias(&ArtifactoryUpload{}, ArtifactoryUploadV1alpha1)
	Scheme.MustRegisterWithAlias(&NexusUpload{}, NexusUploadV1alpha1)
	Scheme.MustRegisterWithAlias(&TransferWgetToOCILocalBlob{}, TransferWgetToOCILocalBlobV1alpha1)
	Scheme.MustRegisterWithAlias(&TransferWgetToCTFLocalBlob{}, TransferWgetToCTFLocalBlobV1alpha1)
	Scheme.MustRegisterWithAlias(&TransferS3ToOCILocalBlob{}, TransferS3ToOCILocalBlobV1alpha1)
	Scheme.MustRegisterWithAlias(&TransferS3ToCTFLocalBlob{}, TransferS3ToCTFLocalBlobV1alpha1)
}
