package discovery

import (
	"testing"

	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/controller/indexes"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/ocm"
)

func TestMapComponentToDiscoveries(t *testing.T) {
	r := require.New(t)
	scheme := newTestScheme(t)

	discoveries := []client.Object{
		&v1alpha1.Discovery{
			ObjectMeta: metav1.ObjectMeta{Name: "root", Namespace: "default"},
			Spec:       v1alpha1.DiscoverySpec{ComponentRef: corev1.LocalObjectReference{Name: "component"}},
		},
		&v1alpha1.Discovery{
			ObjectMeta: metav1.ObjectMeta{Name: "other-namespace", Namespace: "other"},
			Spec:       v1alpha1.DiscoverySpec{ComponentRef: corev1.LocalObjectReference{Name: "component"}},
		},
		&v1alpha1.Discovery{
			ObjectMeta: metav1.ObjectMeta{Name: "other-root", Namespace: "default"},
			Spec:       v1alpha1.DiscoverySpec{ComponentRef: corev1.LocalObjectReference{Name: "other"}},
		},
		&v1alpha1.Discovery{
			ObjectMeta: metav1.ObjectMeta{Name: "configuration", Namespace: "consumer"},
			Spec: v1alpha1.DiscoverySpec{
				ComponentRef: corev1.LocalObjectReference{Name: "other"},
				OCMConfig: []v1alpha1.OCMConfiguration{{
					NamespacedObjectKindReference: v1alpha1.NamespacedObjectKindReference{
						Kind:      v1alpha1.KindComponent,
						Namespace: "default",
						Name:      "component",
					},
				}},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(discoveries...).
		WithIndex(&v1alpha1.Discovery{}, indexes.DiscoveryComponentRef, func(obj client.Object) []string {
			return []string{obj.(*v1alpha1.Discovery).Spec.ComponentRef.Name}
		}).
		WithIndex(&v1alpha1.Discovery{}, ocmConfigIndex, func(obj client.Object) []string {
			discovery := obj.(*v1alpha1.Discovery)
			keys := make([]string, 0, len(discovery.Spec.OCMConfig))
			for _, cfg := range discovery.Spec.OCMConfig {
				namespace := cfg.Namespace
				if namespace == "" {
					namespace = discovery.Namespace
				}
				keys = append(keys, configKey(cfg.Kind, namespace, cfg.Name))
			}
			return keys
		}).
		Build()
	reconciler := &Reconciler{BaseReconciler: &ocm.BaseReconciler{Client: fakeClient}}

	requests := reconciler.mapComponentToDiscoveries(t.Context(), &v1alpha1.Component{
		ObjectMeta: metav1.ObjectMeta{Name: "component", Namespace: "default"},
	})

	r.ElementsMatch([]reconcile.Request{
		{NamespacedName: types.NamespacedName{Name: "root", Namespace: "default"}},
		{NamespacedName: types.NamespacedName{Name: "configuration", Namespace: "consumer"}},
	}, requests)
}
