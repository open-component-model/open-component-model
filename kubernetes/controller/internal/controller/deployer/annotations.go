package deployer

import (
	"context"

	"ocm.software/open-component-model/kubernetes/controller/internal/controller/applyset"
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
	if resource.Status.Resource.Digest != nil {
		anns[annotationResourceDigestPrefix+"value"] = resource.Status.Resource.Digest.Value
		anns[annotationResourceDigestPrefix+"hashAlgorithm"] = resource.Status.Resource.Digest.HashAlgorithm
		anns[annotationResourceDigestPrefix+"normalisationAlgorithm"] = resource.Status.Resource.Digest.NormalisationAlgorithm
	}
	anns[annotationResourceAccess] = string(resource.Status.Resource.Access.Raw)
	anns[annotationComponentName] = resource.Status.Component.Component
	anns[annotationComponentVersion] = resource.Status.Component.Version
}

func (r *Reconciler) setApplySetMetadata(ctx context.Context, obj client.Object, meta applyset.Metadata) error {
	obj.SetLabels(meta.Labels())
	obj.SetAnnotations(meta.Annotations())

	// update object labels and annotations
	err := r.Client.Update(ctx, obj)
	return err
}
