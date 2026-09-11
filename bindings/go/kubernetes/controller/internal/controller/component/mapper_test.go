package component

import (
	"testing"

	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/ocm"
)

func newScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha1.AddToScheme(scheme))

	return scheme
}

func newReconciler(t *testing.T, objects ...client.Object) *Reconciler {
	t.Helper()
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(objects...).Build()

	return &Reconciler{BaseReconciler: &ocm.BaseReconciler{Client: c}}
}

func TestMapToDeletingComponentMapsResourceAndDiscovery(t *testing.T) {
	r := require.New(t)

	component := &v1alpha1.Component{ObjectMeta: metav1.ObjectMeta{
		Name:       "comp",
		Namespace:  "default",
		Finalizers: []string{"test/finalizer"},
	}}
	reconciler := newReconciler(t, component)
	r.NoError(reconciler.Delete(t.Context(), component))

	want := []reconcile.Request{{NamespacedName: types.NamespacedName{Namespace: "default", Name: "comp"}}}

	resource := &v1alpha1.Resource{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
		Spec:       v1alpha1.ResourceSpec{ComponentRef: corev1.LocalObjectReference{Name: "comp"}},
	}
	r.Equal(want, reconciler.mapToDeletingComponent(t.Context(), resource))

	disc := &v1alpha1.Discovery{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
		Spec:       v1alpha1.DiscoverySpec{ComponentRef: corev1.LocalObjectReference{Name: "comp"}},
	}
	r.Equal(want, reconciler.mapToDeletingComponent(t.Context(), disc))
}

func TestMapToDeletingComponentSkipsNonTerminatingComponent(t *testing.T) {
	r := require.New(t)

	component := &v1alpha1.Component{ObjectMeta: metav1.ObjectMeta{Name: "comp", Namespace: "default"}}
	reconciler := newReconciler(t, component)

	disc := &v1alpha1.Discovery{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
		Spec:       v1alpha1.DiscoverySpec{ComponentRef: corev1.LocalObjectReference{Name: "comp"}},
	}
	r.Nil(reconciler.mapToDeletingComponent(t.Context(), disc))
}

func TestMapToDeletingComponentSkipsMissingComponentAndUnsupportedType(t *testing.T) {
	r := require.New(t)

	reconciler := newReconciler(t)

	resource := &v1alpha1.Resource{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
		Spec:       v1alpha1.ResourceSpec{ComponentRef: corev1.LocalObjectReference{Name: "missing"}},
	}
	r.Nil(reconciler.mapToDeletingComponent(t.Context(), resource))

	r.Nil(reconciler.mapToDeletingComponent(t.Context(), &v1alpha1.Component{}))
}

func TestMapToDeletingComponentUpdateRetargetsOldAndNewTerminatingComponents(t *testing.T) {
	r := require.New(t)

	oldComponent := &v1alpha1.Component{ObjectMeta: metav1.ObjectMeta{
		Name:       "old",
		Namespace:  "default",
		Finalizers: []string{"test/finalizer"},
	}}
	newComponent := &v1alpha1.Component{ObjectMeta: metav1.ObjectMeta{
		Name:       "new",
		Namespace:  "default",
		Finalizers: []string{"test/finalizer"},
	}}
	reconciler := newReconciler(t, oldComponent, newComponent)
	r.NoError(reconciler.Delete(t.Context(), oldComponent))
	r.NoError(reconciler.Delete(t.Context(), newComponent))

	eventHandler := handler.EnqueueRequestsFromMapFunc(reconciler.mapToDeletingComponent)
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
	defer queue.ShutDown()

	eventHandler.Update(t.Context(), event.UpdateEvent{
		ObjectOld: &v1alpha1.Discovery{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
			Spec:       v1alpha1.DiscoverySpec{ComponentRef: corev1.LocalObjectReference{Name: oldComponent.Name}},
		},
		ObjectNew: &v1alpha1.Discovery{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
			Spec:       v1alpha1.DiscoverySpec{ComponentRef: corev1.LocalObjectReference{Name: newComponent.Name}},
		},
	}, queue)

	r.Equal(2, queue.Len())
	requests := map[types.NamespacedName]struct{}{}
	for range 2 {
		request, shutdown := queue.Get()
		r.False(shutdown)
		queue.Done(request)
		requests[request.NamespacedName] = struct{}{}
	}
	r.Equal(map[types.NamespacedName]struct{}{
		{Namespace: "default", Name: "old"}: {},
		{Namespace: "default", Name: "new"}: {},
	}, requests)
}
