package transformation

import (
	"context"
	"fmt"

	"github.com/opencontainers/go-digest"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/helm/transformation/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/oci/looseref"
	ociaccess "ocm.software/open-component-model/bindings/go/oci/spec/access"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/repository/integrity"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// SetHelmChartLayerAccess replaces the access of a Helm chart resource with an ociBlob/v1
// access to the chart layer of the Helm OCI image the chart was pushed as. The access names
// that image in imageReference.
// It keeps the recorded digest of the resource: that digest names the chart archive, so a
// copy that addresses the chart layer still matches it and the signature stays valid.
type SetHelmChartLayerAccess struct {
	Scheme *runtime.Scheme
}

func (t *SetHelmChartLayerAccess) Transform(_ context.Context, step runtime.Typed) (runtime.Typed, error) {
	var transformation v1alpha1.SetHelmChartLayerAccess
	if err := t.Scheme.Convert(step, &transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to set helm chart layer access transformation: %w", err)
	}
	spec := transformation.Spec
	if spec == nil {
		return nil, fmt.Errorf("spec is required for set helm chart layer access transformation")
	}
	if spec.Resource == nil {
		return nil, fmt.Errorf("spec.resource is required for set helm chart layer access transformation")
	}
	if spec.ImageReference == "" {
		return nil, fmt.Errorf("spec.imageReference is required for set helm chart layer access transformation")
	}
	layerDigest, err := digest.Parse(spec.ChartLayer.Digest)
	if err != nil {
		return nil, fmt.Errorf("invalid spec.chartLayer.digest %q: %w", spec.ChartLayer.Digest, err)
	}

	ref, err := layerRepositoryReference(spec.ImageReference)
	if err != nil {
		return nil, err
	}

	if err := checkRecordedDigest(spec.Resource, layerDigest); err != nil {
		return nil, err
	}

	layerAccess := ociv1.OCIImageLayer{
		Type:      runtime.NewVersionedType(ociv1.LegacyOCIBlobAccessType, ociv1.LegacyOCIBlobAccessTypeVersion),
		Reference: ref,
		MediaType: spec.ChartLayer.MediaType,
		Digest:    layerDigest,
		Size:      spec.ChartLayer.Size,
		// The layer access cannot name the image, so a consumer that wants the Helm OCI image
		// (helm pull, a deployer) finds it here.
		ImageReference: spec.ImageReference,
	}
	if err := layerAccess.Validate(); err != nil {
		return nil, fmt.Errorf("invalid chart layer access for resource %s: %w", spec.Resource.Name, err)
	}
	rawAccess := runtime.Raw{}
	if err := ociaccess.Scheme.Convert(&layerAccess, &rawAccess); err != nil {
		return nil, fmt.Errorf("failed converting chart layer access of resource %s: %w", spec.Resource.Name, err)
	}

	resource := spec.Resource.DeepCopy()
	resource.Access = &rawAccess
	transformation.Output = &v1alpha1.SetHelmChartLayerAccessOutput{Resource: resource}

	return &transformation, nil
}

// layerRepositoryReference returns the repository of an image reference, scheme kept, without
// tag or digest: a layer is addressed by its own digest, and ociBlob/v1 rejects a tag.
func layerRepositoryReference(imageReference string) (string, error) {
	ref, err := looseref.ParseReference(imageReference)
	if err != nil {
		return "", fmt.Errorf("failed parsing spec.imageReference %q: %w", imageReference, err)
	}
	if ref.Repository == "" {
		return "", fmt.Errorf("spec.imageReference %q has no repository", imageReference)
	}
	ref.Tag = ""
	ref.Digest = ""
	return ref.String(), nil
}

// checkRecordedDigest makes sure the digest the resource records names the chart layer, so the
// copy never writes an access whose content fails verification.
// An exclusion marker or a missing digest is not compared: it makes no claim about the content.
func checkRecordedDigest(resource *descv2.Resource, layerDigest digest.Digest) error {
	if resource.Digest == nil {
		return nil
	}
	recorded, err := integrity.ParseDigest(descruntime.ConvertFromV2Digest(resource.Digest))
	if err != nil {
		return fmt.Errorf("cannot read the recorded digest of resource %s: %w", resource.Name, err)
	}
	if recorded == "" {
		return nil
	}
	if norm := resource.Digest.NormalisationAlgorithm; norm != "" && norm != descruntime.GenericBlobDigestV1 {
		return fmt.Errorf("the recorded %s digest of resource %s does not name the chart archive", norm, resource.Name)
	}
	if recorded != layerDigest {
		return fmt.Errorf("chart layer digest %s does not match the recorded digest %s of resource %s", layerDigest, recorded, resource.Name)
	}
	return nil
}
