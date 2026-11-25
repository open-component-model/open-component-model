package internal

import (
	"crypto"
	"fmt"
	"strings"

	"github.com/opencontainers/go-digest"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	helmv1alpha1 "ocm.software/open-component-model/bindings/go/helm/transformation/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
)

// processHelm copies a Helm chart into the target and returns the CEL spec-field expressions
// of the file buffers it produces, for cleanup.
//
// A chart whose recorded digest must be kept (see keepsChartDigest) is stored as the chart
// archive it names. With an empty ociImageReference the archive becomes a local blob in the
// target. Otherwise the chart is pushed as a Helm OCI image to ociImageReference, and the
// access points at the chart layer of that image, which holds the same archive.
//
// Any other chart is converted to an OCI artifact, which gets a new digest: embedded as a local
// blob with an empty ociImageReference, else pushed to ociImageReference.
func processHelm(resource v2.Resource, id string, val *discoveryValue, tgd *transformv1alpha1.TransformationGraphDefinition, toSpec runtime.Typed, resourceTransformIDs map[int]string, i int, ociImageReference string) ([]string, error) {
	keep, err := keepsChartDigest(val, resource, ociImageReference != "")
	if err != nil {
		return nil, err
	}

	resourceIdentity := resource.ToIdentity()
	resourceID := identityToTransformationID(resourceIdentity)
	getResourceID := fmt.Sprintf("%sGet%s", id, resourceID)
	convertResourceID := fmt.Sprintf("%sConvert%s", id, resourceID)
	addResourceID := fmt.Sprintf("%sAdd%s", id, resourceID)

	unstructured, err := runtime.UnstructuredFromMixedData(map[string]any{
		"resource": resource,
	})
	if err != nil {
		return nil, fmt.Errorf("cannot create unstructured spec for GetHelmChartV1alpha1 transformation: %w", err)
	}

	// Create GetHelmChart transformation
	getChartTransform := transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type:  helmv1alpha1.GetHelmChartV1alpha1,
			ID:    getResourceID,
			Label: getLabel(&val.Descriptor.Component, resource.Name),
		},
		Spec: unstructured,
	}
	tgd.Transformations = append(tgd.Transformations, getChartTransform)

	if keep && ociImageReference == "" {
		// No referenceName: the blob is the chart archive, not an OCI manifest.
		addResourceTransform, err := uploadAsLocalResource(toSpec, val.Descriptor.Component.Name, val.Descriptor.Component.Version, addResourceID, getResourceID, "", addLabel(&val.Descriptor.Component, resource.Name, "LocalBlob", toSpec))
		if err != nil {
			return nil, fmt.Errorf("failed to create chart upload as local resource transformation: %w", err)
		}
		addResourceTransform.Spec.Data["file"] = fmt.Sprintf("${%s.output.chartFile}", getResourceID)
		tgd.Transformations = append(tgd.Transformations, addResourceTransform)
		resourceTransformIDs[i] = addResourceID
		return []string{
			fmt.Sprintf("${%s.spec.file}", addResourceID),
			// Nothing on this chain reads the .prov file, so it is named by the output that
			// wrote it. The cleanup transformer skips it when there is none.
			fmt.Sprintf("${%s.output.?provFile}", getResourceID),
		}, nil
	}

	// convert chart to oci artifact transformation
	convertToOCITransform := transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type:  helmv1alpha1.ConvertHelmToOCIV1alpha1,
			ID:    convertResourceID,
			Label: convertLabel(&val.Descriptor.Component, resource.Name),
		},
		Spec: &runtime.Unstructured{Data: map[string]any{
			"resource":  fmt.Sprintf("${%s.output.resource}", getResourceID),
			"chartFile": fmt.Sprintf("${%s.output.chartFile}", getResourceID),
			"provFile":  fmt.Sprintf("${%s.output.?provFile}", getResourceID),
		}},
	}
	tgd.Transformations = append(tgd.Transformations, convertToOCITransform)

	var addResourceTransform transformv1alpha1.GenericTransformation
	if ociImageReference != "" {
		addResourceTransform = ociAddArtifact(addResourceID, convertResourceID, ociImageReference, addLabel(&val.Descriptor.Component, resource.Name, "OCIArtifact", toSpec))
	} else {
		if addResourceTransform, err = uploadAsLocalResource(toSpec, val.Descriptor.Component.Name, val.Descriptor.Component.Version, addResourceID, convertResourceID, fmt.Sprintf("${%s.output.resource.access.imageReference}", convertResourceID), addLabel(&val.Descriptor.Component, resource.Name, "LocalBlob", toSpec)); err != nil {
			return nil, fmt.Errorf("failed to create oci upload as local resource transformation: %w", err)
		}
	}

	tgd.Transformations = append(tgd.Transformations, addResourceTransform)

	// Track this resource's transformation
	resourceTransformIDs[i] = addResourceID

	if keep {
		setAccessID := fmt.Sprintf("%sSetAccess%s", id, resourceID)
		spec, err := runtime.UnstructuredFromMixedData(map[string]any{
			"resource":       resource,
			"imageReference": pinnedImageReference(addResourceID),
			"chartLayer":     fmt.Sprintf("${%s.output.chartLayer}", convertResourceID),
		})
		if err != nil {
			return nil, fmt.Errorf("cannot create unstructured spec for SetHelmChartLayerAccessV1alpha1 transformation: %w", err)
		}
		tgd.Transformations = append(tgd.Transformations, transformv1alpha1.GenericTransformation{
			TransformationMeta: meta.TransformationMeta{
				Type:  helmv1alpha1.SetHelmChartLayerAccessV1alpha1,
				ID:    setAccessID,
				Label: chartLayerLabel(&val.Descriptor.Component, resource.Name),
			},
			Spec: spec,
		})
		resourceTransformIDs[i] = setAccessID
	}

	return helmFileExpressions(id, resourceID), nil
}

// pinnedImageReference returns the expression for the reference of the image that the
// AddOCIArtifact node id pushed, with its manifest digest. The node pins only an untagged
// reference, but its output always records the manifest digest as the resource digest. A tag
// can move to another image, so the tag alone does not name the pushed one.
func pinnedImageReference(id string) string {
	ref := id + ".output.resource.access.imageReference"
	dig := id + ".output.resource.digest"
	return fmt.Sprintf(`${%[1]s.contains("@") ? %[1]s : %[1]s + "@" + (%[2]s.hashAlgorithm == "SHA-512" ? "sha512:" : "sha256:") + %[2]s.value}`, ref, dig)
}

// keepsChartDigest reports whether the copy of a Helm chart must store the chart archive as it
// is, so that the resource keeps the digest it records. pushed says that the chart goes to the
// target as an OCI image, not as a local blob.
//
// Only a signature, or the reference digest in a parent, covers the digest. Without one the
// chart is converted as before, and the conversion records a new digest.
//
// A covered digest is kept when it is a genericBlobDigest/v1 digest, spelled as OCM writes it
// (SHA-256, lower-case hex): it names the chart archive, and the target checks a stored file
// against exactly this spelling. For any other digest the conversion would add or replace it.
// In a signed component version that is an error: the transfer must never write a component
// version that fails verification.
func keepsChartDigest(val *discoveryValue, resource v2.Resource, pushed bool) (bool, error) {
	desc := val.Descriptor
	signed := len(desc.Signatures) > 0
	if !signed && !val.ReferenceDigest {
		return false, nil
	}

	dig := resource.Digest
	reason := "it has no recorded digest, and the copy would add one"
	if dig != nil {
		norm := dig.NormalisationAlgorithm
		switch {
		case strings.EqualFold(dig.HashAlgorithm, descriptor.NoDigest) || strings.EqualFold(norm, descriptor.ExcludeFromSignature):
			// The digest is excluded from the signature and makes no claim about the content.
			if pushed {
				// The conversion replaces the marker with the manifest digest of the image, so
				// the archive is pushed instead: the chart layer access keeps the resource as it is.
				return true, nil
			}
			// AddLocalResource replaces the marker of a converted chart, but not of an archive,
			// unless it reads the marker as a digest of the archive.
			if norm != "" && norm != descriptor.GenericBlobDigestV1 {
				return true, nil
			}
			reason = fmt.Sprintf("the target cannot store its digest with hash algorithm %s and normalisation algorithm %q; exclude it with normalisation algorithm %s",
				dig.HashAlgorithm, norm, descriptor.ExcludeFromSignature)
		case norm == descriptor.GenericBlobDigestV1 && dig.HashAlgorithm == crypto.SHA256.String() &&
			digest.NewDigestFromEncoded(digest.SHA256, dig.Value).Validate() == nil:
			return true, nil
		default:
			reason = fmt.Sprintf("its recorded digest %s (%s) is not a %s digest of the chart archive with hash algorithm %s and a lower-case hex value, and the copy would replace it",
				norm, dig.HashAlgorithm, descriptor.GenericBlobDigestV1, crypto.SHA256.String())
		}
	}
	if !signed {
		return false, nil
	}
	return false, fmt.Errorf("cannot copy the Helm chart of signed component version %s:%s: %s; keep the resource by reference (a reference.uploader entry that matches it) or transfer without copying it",
		desc.Component.Name, desc.Component.Version, reason)
}
