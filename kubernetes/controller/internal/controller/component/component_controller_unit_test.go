package component

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/hashicorp/golang-lru/v2/expirable"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci"
	ocirepository "ocm.software/open-component-model/bindings/go/oci/repository"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/kubernetes/controller/internal/ocm"
	"ocm.software/open-component-model/kubernetes/controller/internal/resolution"
	"ocm.software/open-component-model/kubernetes/controller/internal/resolution/workerpool"
)

func newComponentReconciler(fakeClient client.Client, scheme *runtime.Scheme) *Reconciler {
	return &Reconciler{
		BaseReconciler: &ocm.BaseReconciler{
			Client:        fakeClient,
			Scheme:        scheme,
			EventRecorder: &record.FakeRecorder{Events: make(chan string, 100)},
		},
	}
}

func TestReconcile_RepositoryNotReady_ReturnsNoRequeue(t *testing.T) {
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

	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(result).To(Equal(ctrl.Result{}))
}

func TestReconcile_RepositoryBeingDeleted_ReturnsNoRequeue(t *testing.T) {
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

	g.Expect(err).ToNot(HaveOccurred())
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
	g.Expect(err).ToNot(HaveOccurred())

	updated := &v1alpha1.Component{}
	g.Expect(fakeClient.Get(ctx, types.NamespacedName{Name: "test-component", Namespace: "default"}, updated)).To(Succeed())
	g.Expect(apimeta.IsStatusConditionTrue(updated.GetConditions(), v1alpha1.ReadyCondition)).To(BeFalse())
}

// TestResolutionInProgress_UnitTest tests that the first reconciliation returns ResolutionInProgress
// when a component version resolution is started. This test uses a fake client to avoid race conditions
// with the fast event-driven reconciliation. This cannot be tested reliably over envtest because the
// reconcile will be too fast to get the InProgress status.
func TestResolutionInProgress_UnitTest(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	scheme := runtime.NewScheme()
	g.Expect(corev1.AddToScheme(scheme)).To(Succeed())
	g.Expect(v1alpha1.AddToScheme(scheme)).To(Succeed())

	namespace := "test-namespace"
	componentName := "ocm.software/test-component"
	repositoryName := "test-repo"
	componentObjName := "test-component-obj"
	version := "1.0.0"

	tmpDir := t.TempDir()
	repoSpec := &ctf.Repository{
		Type:       ocmruntime.Type{Version: "v1", Name: "ctf"},
		FilePath:   tmpDir,
		AccessMode: ctf.AccessModeReadWrite,
	}
	var repo *oci.Repository
	repo, err := ocirepository.NewFromCTFRepoV1(ctx, repoSpec)
	g.Expect(err).ToNot(HaveOccurred())

	desc := &descruntime.Descriptor{
		Component: descruntime.Component{
			ComponentMeta: descruntime.ComponentMeta{
				ObjectMeta: descruntime.ObjectMeta{
					Name:    componentName,
					Version: version,
				},
			},
			Provider: descruntime.Provider{Name: "ocm.software"},
		},
	}
	g.Expect(repo.AddComponentVersion(ctx, desc)).To(Succeed())

	specData, err := json.Marshal(repoSpec)
	g.Expect(err).ToNot(HaveOccurred())

	repository := &v1alpha1.Repository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      repositoryName,
			Namespace: namespace,
		},
		Spec: v1alpha1.RepositorySpec{
			RepositorySpec: &apiextensionsv1.JSON{
				Raw: specData,
			},
			Interval: metav1.Duration{Duration: time.Minute * 10},
		},
		Status: v1alpha1.RepositoryStatus{},
	}
	apimeta.SetStatusCondition(&repository.Status.Conditions, metav1.Condition{
		Type:    v1alpha1.ReadyCondition,
		Status:  metav1.ConditionTrue,
		Reason:  v1alpha1.SucceededReason,
		Message: "ready",
	})

	component := &v1alpha1.Component{
		ObjectMeta: metav1.ObjectMeta{
			Name:      componentObjName,
			Namespace: namespace,
		},
		Spec: v1alpha1.ComponentSpec{
			RepositoryRef: corev1.LocalObjectReference{
				Name: repositoryName,
			},
			Component: componentName,
			Semver:    "1.0.0",
			Interval:  metav1.Duration{Duration: time.Minute * 10},
		},
		Status: v1alpha1.ComponentStatus{},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(repository, component).
		WithStatusSubresource(&v1alpha1.Component{}, &v1alpha1.Repository{}).
		Build()

	logger := logr.Discard()
	const unlimited = 0
	ttl := time.Minute * 30
	resolverCache := expirable.NewLRU[string, *workerpool.Result](unlimited, nil, ttl)

	workerPool := workerpool.NewWorkerPool(workerpool.PoolOptions{
		WorkerCount: 1,
		QueueSize:   10,
		Logger:      &logger,
		Client:      fakeClient,
		Cache:       resolverCache,
	})

	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		_ = workerPool.Start(workerCtx)
	}()

	pm := manager.NewPluginManager(ctx)
	ocmScheme := ocmruntime.NewScheme()
	ocmScheme.MustRegisterWithAlias(&ctf.Repository{},
		ocmruntime.NewVersionedType(ctf.Type, ctf.Version),
		ocmruntime.NewUnversionedType(ctf.Type),
		ocmruntime.NewVersionedType(ctf.ShortType, ctf.Version),
		ocmruntime.NewUnversionedType(ctf.ShortType),
		ocmruntime.NewVersionedType(ctf.ShortType2, ctf.Version),
		ocmruntime.NewUnversionedType(ctf.ShortType2),
	)
	repositoryProvider := provider.NewComponentVersionRepositoryProvider(provider.WithScheme(ocmScheme))
	g.Expect(pm.ComponentVersionRepositoryRegistry.RegisterInternalComponentVersionRepositoryPlugin(repositoryProvider)).To(Succeed())

	resolver := resolution.NewResolver(fakeClient, &logger, workerPool, pm)
	eventRecorder := &record.FakeRecorder{
		Events: make(chan string, 100),
	}
	reconciler := &Reconciler{
		BaseReconciler: &ocm.BaseReconciler{
			Client:        fakeClient,
			Scheme:        scheme,
			EventRecorder: eventRecorder,
		},
		Resolver:      resolver,
		PluginManager: pm,
	}

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      componentObjName,
			Namespace: namespace,
		},
	}

	_, err = reconciler.Reconcile(ctx, req)
	g.Expect(err).ToNot(HaveOccurred())
	_, err = reconciler.Reconcile(ctx, req)
	g.Expect(err).ToNot(HaveOccurred())
	updatedComponent := &v1alpha1.Component{}
	g.Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(component), updatedComponent)).To(Succeed())

	cond := apimeta.FindStatusCondition(updatedComponent.GetConditions(), v1alpha1.ReadyCondition)
	g.Expect(cond).ToNot(BeNil())
	g.Expect(cond.Reason).To(Equal(v1alpha1.ResolutionInProgress),
		"expected component ready-condition reason to be %s, got %s",
		v1alpha1.ResolutionInProgress, cond.Reason)
	g.Expect(cond).ToNot(BeNil())
	g.Expect(cond.Message).To(ContainSubstring("resolution in progress"))

	close(eventRecorder.Events)
	var events []string
	for event := range eventRecorder.Events {
		events = append(events, event)
	}
	g.Expect(events).To(ContainElement(ContainSubstring(v1alpha1.ResolutionInProgress)))
}
