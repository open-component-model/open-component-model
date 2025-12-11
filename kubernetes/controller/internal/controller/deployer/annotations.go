package deployer

import (
	"fmt"
	"regexp"

	v1 "ocm.software/ocm/api/ocm/compdesc/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	deliveryv1alpha1 "ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
)

const (
	// annotationResourceDigestPrefix is the annotation prefix used to store an applicable resource digest
	// on deployed objects.
	annotationResourceDigestPrefix = "digest.resource.delivery.ocm.software/"
	// annotationResourceAccess is the annotation used to store the resource access on deployed objects.
	annotationResourceAccess = "resource.delivery.ocm.software/access"
	// annotationResourceIdentityPrefix is the annotation prefix used to store the ocm identity of a resource
	// on deployed objects.
	annotationResourceIdentityPrefix = "identity.resource.delivery.ocm.software/"
	// annotationComponentName is the annotation used to store the component name of a deployed resource.
	annotationComponentName = "component.delivery.ocm.software/name"
	// annotationComponentVersion is the annotation used to store the component version of a deployed resource.
	annotationComponentVersion = "component.delivery.ocm.software/version"
)

var digestSpecStringPattern = regexp.MustCompile(`^(?P<algo>[\w\-]+):(?P<digest>[a-fA-F0-9]+)\[(?P<norm>[\w\/]+)\]$`)

// TODO(jakobmoellerdev): currently digests are stored as strings in resource status, we should really consider storing them natively...
func digestSpec(s string) (v1.DigestSpec, error) {
	matches := digestSpecStringPattern.FindStringSubmatch(s)
	if expectedMatches := 4; len(matches) != expectedMatches {
		return v1.DigestSpec{}, fmt.Errorf("invalid digest spec format: %s", s)
	}

	digestSpec := v1.DigestSpec{}
	for i, name := range digestSpecStringPattern.SubexpNames() {
		switch name {
		case "algo":
			digestSpec.HashAlgorithm = matches[i]
		case "digest":
			digestSpec.Value = matches[i]
		case "norm":
			digestSpec.NormalisationAlgorithm = matches[i]
		}
	}

	return digestSpec, nil
}

func setOwnershipAnnotations(obj client.Object, resource *deliveryv1alpha1.Resource) {
	anns := map[string]string{}
	if existing := obj.GetAnnotations(); existing != nil {
		anns = existing
	}
	defer func() {
		obj.SetAnnotations(anns)
	}()

	// Set the annotations for the resource identity and digest.
	anns[annotationResourceIdentityPrefix+"name"] = resource.Status.Resource.Name
	anns[annotationResourceIdentityPrefix+"version"] = resource.Status.Resource.Version
	for key, value := range resource.Status.Resource.ExtraIdentity {
		anns[annotationResourceIdentityPrefix+key] = value
	}
	if resource.Status.Resource.Digest != "" {
		if spec, err := digestSpec(resource.Status.Resource.Digest); err == nil {
			anns[annotationResourceDigestPrefix+"value"] = spec.Value
			anns[annotationResourceDigestPrefix+"hashAlgorithm"] = spec.HashAlgorithm
			anns[annotationResourceDigestPrefix+"normalisationAlgorithm"] = spec.NormalisationAlgorithm
		} else {
			anns[annotationResourceDigestPrefix+"raw"] = resource.Status.Resource.Digest
		}
	}
	anns[annotationResourceAccess] = string(resource.Status.Resource.Access.Raw)
	anns[annotationComponentName] = resource.Status.Component.Component
	anns[annotationComponentVersion] = resource.Status.Component.Version
}
