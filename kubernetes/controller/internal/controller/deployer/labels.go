package deployer

import (
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"

	deliveryv1alpha1 "ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
)

// see https://kubernetes.io/docs/concepts/overview/working-with-objects/common-labels/#labels
const (
	// The tool being used to manage the operation of an application.
	managedByLabel = "app.kubernetes.io/managed-by"
	// The current version of the application (e.g., a SemVer 1.0, revision hash, etc.)
	versionLabel = "app.kubernetes.io/version"
	// The name of a higher level application this one is part of.
	partOfLabel = "app.kubernetes.io/part-of"
	// A unique name identifying the instance of an application.
	instanceLabel = "app.kubernetes.io/instance"
	// The name of the application.
	nameLabel = "app.kubernetes.io/name"
	// The component within the architecture.
	componentLabel = "app.kubernetes.io/component"
)

// These are common labels.
func setOwnershipLabels(obj client.Object, resource *deliveryv1alpha1.Resource, deployer *deliveryv1alpha1.Deployer) {
	limit := func(v string) string {
		if len(v) > validation.LabelValueMaxLength {
			return v[:validation.LabelValueMaxLength]
		}

		return v
	}

	var lbls map[string]string
	if existing := obj.GetLabels(); existing != nil {
		lbls = existing
	} else {
		lbls = make(map[string]string)
	}
	defer func() {
		obj.SetLabels(lbls)
	}()

	// the component within the architecture is always the name of the resource in k8s.
	lbls[componentLabel] = limit(resource.GetName())
	// the name of the deployed object is the name of the resource in k8s
	lbls[nameLabel] = limit(resource.GetName())
	// the version of the resource determines the version of the deployed object.
	lbls[versionLabel] = limit(resource.Status.Resource.Version)
	// the actual resource instance also determines the object instance.
	lbls[instanceLabel] = limit(string(resource.GetUID()))
	// the name of the higher level component the object is part of is always the deployer.
	lbls[partOfLabel] = limit(deployer.GetName())
	// the object is always managed by the deployer controller.
	lbls[managedByLabel] = deployerManager
}
