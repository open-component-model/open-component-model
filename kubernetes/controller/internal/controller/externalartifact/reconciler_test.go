package externalartifact

import (
	"context"
	"testing"

	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/kubernetes/controller/internal/ocm"
)

func newReconciler(t *testing.T, c client.Client) *Reconciler {
	t.Helper()
	base := t.TempDir()
	storage, err := NewStorage(base, "example.svc.")
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}

	return &Reconciler{
		BaseReconciler: &ocm.BaseReconciler{
			Client:        c,
			Scheme:        c.Scheme(),
			EventRecorder: record.NewFakeRecorder(16),
		},
		Storage: storage,
	}
}

func readyResource(name, namespace string) *v1alpha1.Resource {
	r := &v1alpha1.Resource{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
	}
	// Mark Ready with resolved resource + component status so the reconciler
	// proceeds to artifact production.
	apimeta.SetStatusCondition(&r.Status.Conditions, metav1.Condition{
		Type:   v1alpha1.ReadyCondition,
		Status: metav1.ConditionTrue,
		Reason: v1alpha1.SucceededReason,
	})
	r.Status.Resource = &v1alpha1.ResourceInfo{Name: name, Type: "kustomization"}
	r.Status.Component = &v1alpha1.ComponentInfo{Component: "c", Version: "1.0.0"}

	return r
}

func TestReconcileSkipsWhenResourceMissing(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).Build()
	r := newReconciler(t, c)

	res, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: client.ObjectKey{Namespace: "ns", Name: "gone"},
	})
	if err != nil {
		t.Fatalf("Reconcile on missing resource should not error: %v", err)
	}
	if res.Requeue || res.RequeueAfter != 0 {
		t.Errorf("expected empty result for missing resource, got %+v", res)
	}
}

func TestReconcileSkipsUnreadyResource(t *testing.T) {
	unready := &v1alpha1.Resource{ObjectMeta: metav1.ObjectMeta{Name: "x", Namespace: "ns"}}
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(unready).Build()
	r := newReconciler(t, c)

	res, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: client.ObjectKey{Namespace: "ns", Name: "x"},
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if res.Requeue {
		t.Errorf("unready resource should not requeue, got %+v", res)
	}
	// No ExternalArtifact should have been created for an unready resource.
	ea := &sourcev1.ExternalArtifact{}
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: "ns", Name: "x"}, ea); err == nil {
		t.Error("no ExternalArtifact expected for an unready resource")
	}
}

func TestReconcileAddsFinalizerFirst(t *testing.T) {
	res := readyResource("app", "ns")
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(res).Build()
	r := newReconciler(t, c)

	result, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: client.ObjectKey{Namespace: "ns", Name: "app"},
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if !result.Requeue {
		t.Error("expected requeue after finalizer addition")
	}

	got := &v1alpha1.Resource{}
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: "ns", Name: "app"}, got); err != nil {
		t.Fatalf("get resource: %v", err)
	}
	found := false
	for _, f := range got.GetFinalizers() {
		if f == ExternalArtifactFinalizer {
			found = true
		}
	}
	if !found {
		t.Error("finalizer should have been added on first reconcile")
	}
}

func TestReconcileFetchFailureSetsNotReady(t *testing.T) {
	// A Ready resource with the finalizer already present drives straight into
	// artifact production, which fails because the (empty) component status
	// cannot resolve any OCM content — exercising the error path.
	res := readyResource("app", "ns")
	res.Finalizers = []string{ExternalArtifactFinalizer}
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(res).Build()
	r := newReconciler(t, c)

	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: client.ObjectKey{Namespace: "ns", Name: "app"},
	})
	if err == nil {
		t.Fatal("expected reconcile to fail when OCM content cannot be resolved")
	}

	// The ExternalArtifact should exist with Ready=False.
	ea := &sourcev1.ExternalArtifact{}
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: "ns", Name: "app"}, ea); err != nil {
		t.Fatalf("ExternalArtifact should have been created: %v", err)
	}
	if apimeta.IsStatusConditionTrue(ea.Status.Conditions, "Ready") {
		t.Error("Ready should be False after a fetch failure")
	}
}
