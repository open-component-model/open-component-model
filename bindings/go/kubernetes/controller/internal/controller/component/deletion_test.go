package component

import (
	"testing"

	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/controller/indexes"
)

func TestReconcileDeleteDiscoveryReferences(t *testing.T) {
	r := require.New(t)
	scheme := newScheme(t)
	component := &v1alpha1.Component{ObjectMeta: metav1.ObjectMeta{
		Name:       "component",
		Namespace:  "default",
		Finalizers: []string{v1alpha1.ComponentFinalizer},
	}}
	blocking := &v1alpha1.Discovery{
		ObjectMeta: metav1.ObjectMeta{Name: "blocking", Namespace: "default"},
		Spec:       v1alpha1.DiscoverySpec{ComponentRef: corev1.LocalObjectReference{Name: component.Name}},
	}
	otherNamespace := &v1alpha1.Discovery{
		ObjectMeta: metav1.ObjectMeta{Name: "non-blocking", Namespace: "other"},
		Spec:       v1alpha1.DiscoverySpec{ComponentRef: corev1.LocalObjectReference{Name: component.Name}},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(component, blocking, otherNamespace).
		WithIndex(&v1alpha1.Resource{}, resourceIndex, func(obj client.Object) []string {
			return []string{obj.(*v1alpha1.Resource).Spec.ComponentRef.Name}
		}).
		WithIndex(&v1alpha1.Discovery{}, indexes.DiscoveryComponentRef, func(obj client.Object) []string {
			return []string{obj.(*v1alpha1.Discovery).Spec.ComponentRef.Name}
		}).
		Build()
	reconciler := newComponentReconciler(fakeClient, scheme)

	current := &v1alpha1.Component{}
	r.NoError(fakeClient.Get(t.Context(), client.ObjectKeyFromObject(component), current))
	err := reconciler.reconcileDelete(t.Context(), current)
	r.ErrorContains(err, "default/blocking")
	r.NotContains(err.Error(), "other/non-blocking")

	r.NoError(fakeClient.Delete(t.Context(), blocking))
	r.NoError(fakeClient.Get(t.Context(), client.ObjectKeyFromObject(component), current))
	r.NoError(reconciler.reconcileDelete(t.Context(), current))

	updated := &v1alpha1.Component{}
	r.NoError(fakeClient.Get(t.Context(), client.ObjectKeyFromObject(component), updated))
	r.NotContains(updated.Finalizers, v1alpha1.ComponentFinalizer)
}
