package component

import (
	"context"
	"testing"
	"time"

	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/ocm"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/test"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
)

func newComponentReconciler(fakeClient client.Client, scheme *runtime.Scheme) *Reconciler {
	return &Reconciler{
		BaseReconciler: &ocm.BaseReconciler{
			Client:        fakeClient,
			Scheme:        scheme,
			EventRecorder: &record.FakeRecorder{Events: make(chan string, 100)},
			// These tests never actually call manager, so a static one is good enough.
			NewPluginManager: test.StaticPluginManager(manager.NewPluginManager(context.Background())),
		},
	}
}

func TestReconcile_RepositoryNotReady_RequeuesWithBackoff(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	scheme := runtime.NewScheme()
	g.Expect(corev1.AddToScheme(scheme)).To(Succeed())
	g.Expect(v1alpha1.AddToScheme(scheme)).To(Succeed())

	repo := &v1alpha1.Repository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-repo",
			Namespace: "default",
		},
		Spec: v1alpha1.RepositorySpec{
			Interval: metav1.Duration{Duration: time.Minute},
		},
	}

	component := &v1alpha1.Component{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-component",
			Namespace:  "default",
			Finalizers: []string{v1alpha1.ComponentFinalizer},
		},
		Spec: v1alpha1.ComponentSpec{
			RepositoryRef: corev1.LocalObjectReference{Name: "test-repo"},
			Component:     "ocm.software/test",
			Semver:        "1.0.0",
			Interval:      metav1.Duration{Duration: time.Minute},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(repo, component).
		WithStatusSubresource(&v1alpha1.Component{}, &v1alpha1.Repository{}).
		Build()

	result, err := newComponentReconciler(fakeClient, scheme).Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-component", Namespace: "default"},
	})

	// The repository not being ready is surfaced as an error so controller-runtime
	// requeues the Component with exponential backoff rather than Forgetting it.
	g.Expect(err).To(HaveOccurred())
	g.Expect(result).To(Equal(ctrl.Result{}))
}

func TestReconcile_RepositoryBeingDeleted_RequeuesWithBackoff(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	scheme := runtime.NewScheme()
	g.Expect(corev1.AddToScheme(scheme)).To(Succeed())
	g.Expect(v1alpha1.AddToScheme(scheme)).To(Succeed())

	now := metav1.Now()
	repo := &v1alpha1.Repository{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-repo",
			Namespace:         "default",
			DeletionTimestamp: &now,
			Finalizers:        []string{"prevent-deletion"},
		},
		Spec: v1alpha1.RepositorySpec{
			Interval: metav1.Duration{Duration: time.Minute},
		},
	}
	apimeta.SetStatusCondition(&repo.Status.Conditions, metav1.Condition{
		Type:    v1alpha1.ReadyCondition,
		Status:  metav1.ConditionTrue,
		Reason:  v1alpha1.SucceededReason,
		Message: "ready",
	})

	component := &v1alpha1.Component{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-component",
			Namespace:  "default",
			Finalizers: []string{v1alpha1.ComponentFinalizer},
		},
		Spec: v1alpha1.ComponentSpec{
			RepositoryRef: corev1.LocalObjectReference{Name: "test-repo"},
			Component:     "ocm.software/test",
			Semver:        "1.0.0",
			Interval:      metav1.Duration{Duration: time.Minute},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(repo, component).
		WithStatusSubresource(&v1alpha1.Component{}, &v1alpha1.Repository{}).
		Build()

	result, err := newComponentReconciler(fakeClient, scheme).Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-component", Namespace: "default"},
	})

	// The repository being deleted is surfaced as an error so controller-runtime
	// requeues the Component with exponential backoff rather than Forgetting it.
	g.Expect(err).To(HaveOccurred())
	g.Expect(result).To(Equal(ctrl.Result{}))
}

func TestReconcile_RepositoryNotFound_ReturnsError(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	scheme := runtime.NewScheme()
	g.Expect(corev1.AddToScheme(scheme)).To(Succeed())
	g.Expect(v1alpha1.AddToScheme(scheme)).To(Succeed())

	component := &v1alpha1.Component{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-component",
			Namespace:  "default",
			Finalizers: []string{v1alpha1.ComponentFinalizer},
		},
		Spec: v1alpha1.ComponentSpec{
			RepositoryRef: corev1.LocalObjectReference{Name: "nonexistent-repo"},
			Component:     "ocm.software/test",
			Semver:        "1.0.0",
			Interval:      metav1.Duration{Duration: time.Minute},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(component).
		WithStatusSubresource(&v1alpha1.Component{}).
		Build()

	_, err := newComponentReconciler(fakeClient, scheme).Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-component", Namespace: "default"},
	})

	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("failed to get ready repository"))
}

func TestReconcile_ComponentNotFound_ReturnsNoError(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	scheme := runtime.NewScheme()
	g.Expect(corev1.AddToScheme(scheme)).To(Succeed())
	g.Expect(v1alpha1.AddToScheme(scheme)).To(Succeed())

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	result, err := newComponentReconciler(fakeClient, scheme).Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "nonexistent", Namespace: "default"},
	})

	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(result).To(Equal(ctrl.Result{}))
}

func TestReconcile_RepositoryNotReady_ComponentMarkedNotReady(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	scheme := runtime.NewScheme()
	g.Expect(corev1.AddToScheme(scheme)).To(Succeed())
	g.Expect(v1alpha1.AddToScheme(scheme)).To(Succeed())

	repo := &v1alpha1.Repository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-repo",
			Namespace: "default",
		},
		Spec: v1alpha1.RepositorySpec{
			Interval: metav1.Duration{Duration: time.Minute},
		},
	}

	component := &v1alpha1.Component{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-component",
			Namespace:  "default",
			Finalizers: []string{v1alpha1.ComponentFinalizer},
		},
		Spec: v1alpha1.ComponentSpec{
			RepositoryRef: corev1.LocalObjectReference{Name: "test-repo"},
			Component:     "ocm.software/test",
			Semver:        "1.0.0",
			Interval:      metav1.Duration{Duration: time.Minute},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(repo, component).
		WithStatusSubresource(&v1alpha1.Component{}, &v1alpha1.Repository{}).
		Build()

	_, err := newComponentReconciler(fakeClient, scheme).Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-component", Namespace: "default"},
	})
	// The repository not being ready is surfaced as an error so controller-runtime
	// requeues the Component with exponential backoff rather than Forgetting it.
	g.Expect(err).To(HaveOccurred())

	updated := &v1alpha1.Component{}
	g.Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "test-component", Namespace: "default"}, updated)).To(Succeed())
	g.Expect(apimeta.IsStatusConditionTrue(updated.GetConditions(), v1alpha1.ReadyCondition)).To(BeFalse())
}
