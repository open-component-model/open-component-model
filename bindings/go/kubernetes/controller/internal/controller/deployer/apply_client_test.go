package deployer

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/ocm"
)

func TestApplyClientConflictReader(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	managerClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	apiReader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: defaultServiceAccount, Namespace: "default"},
	}).Build()

	newReconciler := func(namespaced bool) *Reconciler {
		return &Reconciler{
			BaseReconciler:     &ocm.BaseReconciler{Client: managerClient, Scheme: scheme},
			Namespaced:         namespaced,
			apiReader:          apiReader,
			restConfig:         &rest.Config{Host: "https://127.0.0.1:6443"},
			resourceRESTMapper: meta.NewDefaultRESTMapper(nil),
		}
	}

	t.Run("Deployer reads through the uncached API reader", func(t *testing.T) {
		r := require.New(t)

		deployer := &v1alpha1.Deployer{ObjectMeta: metav1.ObjectMeta{Name: "deployer", Namespace: "default"}}
		c, reader, err := newReconciler(false).applyClient(t.Context(), deployer)
		r.NoError(err)
		r.Same(managerClient, c)
		r.Same(apiReader, reader)
	})

	t.Run("NamespacedDeployer reads with its impersonating client", func(t *testing.T) {
		r := require.New(t)

		deployer := &v1alpha1.NamespacedDeployer{ObjectMeta: metav1.ObjectMeta{Name: "deployer", Namespace: "default"}}
		c, reader, err := newReconciler(true).applyClient(t.Context(), deployer)
		r.NoError(err)
		r.NotSame(managerClient, c)
		r.Same(c, reader)
	})
}
