package deployer

import (
	"context"
	"errors"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/controller/applyset"
)

func TestCreateApplySetConflictReader(t *testing.T) {
	errApplyClient := errors.New("listed with the apply client")
	errAPIReader := errors.New("listed with the API reader")

	failingList := func(err error) client.WithWatch {
		return fake.NewClientBuilder().WithInterceptorFuncs(interceptor.Funcs{
			List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
				return err
			},
		}).Build()
	}

	mapper := meta.NewDefaultRESTMapper(nil)
	mapper.Add(corev1.SchemeGroupVersion.WithKind("ConfigMap"), meta.RESTScopeNamespace)

	deployer := &v1alpha1.Deployer{ObjectMeta: metav1.ObjectMeta{Name: "deployer", Namespace: "default"}}
	deployer.SetGroupVersionKind(v1alpha1.GroupVersion.WithKind(v1alpha1.KindDeployer))
	namespacedDeployer := &v1alpha1.NamespacedDeployer{ObjectMeta: metav1.ObjectMeta{Name: "deployer", Namespace: "default"}}
	namespacedDeployer.SetGroupVersionKind(v1alpha1.GroupVersion.WithKind(v1alpha1.KindNamespacedDeployer))

	for _, tc := range []struct {
		name       string
		namespaced bool
		deployer   v1alpha1.DeployerObject
		want       error
	}{
		{name: "Deployer lists through the API reader", deployer: deployer, want: errAPIReader},
		{name: "NamespacedDeployer lists through its apply client", namespaced: true, deployer: namespacedDeployer, want: errApplyClient},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)

			reconciler := &Reconciler{
				Namespaced:         tc.namespaced,
				apiReader:          failingList(errAPIReader),
				resourceRESTMapper: mapper,
			}
			cm := &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata":   map[string]any{"name": "cm", "namespace": "default"},
			}}

			_, err := reconciler.createApplySet(failingList(errApplyClient), tc.deployer, logr.Discard()).
				Apply(t.Context(), []applyset.Resource{{ID: "cm", Object: cm}}, applyset.ApplyMode{})
			r.ErrorIs(err, tc.want)
		})
	}
}
