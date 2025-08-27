package dynamic

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TransformPartialObjectMetadata transforms any objects received from the api-server
// before adding them to the cache.
func TransformPartialObjectMetadata(obj any) (any, error) {
	m, err := meta.Accessor(obj)
	if err != nil {
		return nil, fmt.Errorf("unable to access metadata: %w", err)
	}
	t, err := meta.TypeAccessor(obj)
	if err != nil {
		return nil, fmt.Errorf("unable to access type: %w", err)
	}

	return &metav1.PartialObjectMetadata{
		TypeMeta: metav1.TypeMeta{
			Kind:       t.GetKind(),
			APIVersion: t.GetAPIVersion(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:                       m.GetName(),
			GenerateName:               m.GetGenerateName(),
			Namespace:                  m.GetNamespace(),
			UID:                        m.GetUID(),
			ResourceVersion:            m.GetResourceVersion(),
			Generation:                 m.GetGeneration(),
			CreationTimestamp:          m.GetCreationTimestamp(),
			DeletionTimestamp:          m.GetDeletionTimestamp(),
			DeletionGracePeriodSeconds: m.GetDeletionGracePeriodSeconds(),
			Labels:                     m.GetLabels(),
			Annotations:                m.GetAnnotations(),
			OwnerReferences:            m.GetOwnerReferences(),
			Finalizers:                 m.GetFinalizers(),
			ManagedFields:              m.GetManagedFields(),
		},
	}, nil
}
