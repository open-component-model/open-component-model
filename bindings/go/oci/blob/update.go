package blob

import (
	"fmt"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	internaldigest "ocm.software/open-component-model/bindings/go/oci/internal/digest"
)

// UpdateArtifactWithInformationFromBlob updates the artifact with information from the blob.
// It defaults an ordinary resource's missing digest from the blob's byte checksum.
// Layout archives require a normalized manifest digest, which is supplied by packing.
// This is currently only possible with resources because sources do not have corresponding properties
// that could be defaulted.
func UpdateArtifactWithInformationFromBlob(artifact descriptor.Artifact, b blob.ReadOnlyBlob) error {
	//nolint:gocritic // we have only resource for now but there might be more in the future
	switch typed := artifact.(type) {
	case *descriptor.Resource:
		if typed.Digest == nil && !isOCILayout(artifact, b, "") {
			if digAware, ok := b.(blob.DigestAware); ok {
				if dig, ok := digAware.Digest(); ok {
					digSpec, err := digestSpec(dig)
					if err != nil {
						return fmt.Errorf("failed to parse digest spec from blob: %w", err)
					}
					if digSpec != nil {
						digSpec.NormalisationAlgorithm = internaldigest.GenericBlobDigestV1
					}
					typed.Digest = digSpec
				}
			}
		}
	}

	return nil
}
