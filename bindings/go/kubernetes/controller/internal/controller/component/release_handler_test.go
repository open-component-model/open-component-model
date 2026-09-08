package component

import (
	"testing"

	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
)

func TestDiscoveryReleaseHandlerUpdateEnqueuesOldAndNewTerminatingComponents(t *testing.T) {
	r := require.New(t)

	scheme := runtime.NewScheme()
	r.NoError(v1alpha1.AddToScheme(scheme))
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
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(oldComponent, newComponent).Build()
	r.NoError(client.Delete(t.Context(), oldComponent))
	r.NoError(client.Delete(t.Context(), newComponent))
	handler := &discoveryReleaseHandler{client: client}
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
	defer queue.ShutDown()

	handler.Update(t.Context(), event.UpdateEvent{
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
