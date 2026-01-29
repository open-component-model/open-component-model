package applyset

import (
	"context"
	"regexp"
	"testing"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// testParent is a minimal implementation of the parent interface for testing.
type testParent struct {
	metav1.ObjectMeta
	gvk schema.GroupVersionKind
}

func (p *testParent) GroupVersionKind() schema.GroupVersionKind {
	return p.gvk
}

func (p *testParent) SetGroupVersionKind(gvk schema.GroupVersionKind) {
	p.gvk = gvk
}

func newTestParent(gvk schema.GroupVersionKind) *testParent {
	return &testParent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-instance",
			Namespace: "default",
			UID:       types.UID("test-parent-uid"),
		},
		gvk: gvk,
	}
}

func newTestRESTMapper() meta.RESTMapper {
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{
		{Group: "", Version: "v1"},
		{Group: "apps", Version: "v1"},
	})
	mapper.Add(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}, meta.RESTScopeNamespace)
	mapper.Add(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Secret"}, meta.RESTScopeNamespace)
	mapper.Add(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}, meta.RESTScopeNamespace)
	mapper.Add(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Namespace"}, meta.RESTScopeRoot)
	return mapper
}

func newConfigMap(name, namespace string) *unstructured.Unstructured {
	cm := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": namespace,
			},
			"data": map[string]interface{}{
				"key": "value",
			},
		},
	}
	cm.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})
	return cm
}

func newSecret(name, namespace string) *unstructured.Unstructured {
	secret := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": namespace,
			},
			"stringData": map[string]interface{}{
				"password": "secret",
			},
		},
	}
	secret.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Secret"})
	return secret
}

func newNamespace(name string) *unstructured.Unstructured {
	ns := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Namespace",
			"metadata": map[string]interface{}{
				"name": name,
			},
		},
	}
	ns.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Namespace"})
	return ns
}

// newFakeClient creates a controller-runtime fake client with the given objects.
// The fake client automatically handles Server-Side Apply (SSA).
func newFakeClient(objs ...client.Object) client.Client {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	builder := fake.NewClientBuilder().WithScheme(scheme)
	if len(objs) > 0 {
		builder = builder.WithObjects(objs...)
	}
	return builder.Build()
}

func TestApply_BasicSSA(t *testing.T) {
	ctx := context.Background()
	mapper := newTestRESTMapper()
	parent := newTestParent(schema.GroupVersionKind{
		Group: "delivery.ocm.software", Version: "v1alpha1", Kind: "TestKind",
	})

	tests := map[string]struct {
		resources    []Resource
		wantApplied  int
		wantNoErrors bool
	}{
		"single resource": {
			resources: []Resource{
				{ID: "cm1", Object: newConfigMap("cm1", "default")},
			},
			wantApplied:  1,
			wantNoErrors: true,
		},
		"multiple resources": {
			resources: []Resource{
				{ID: "cm1", Object: newConfigMap("cm1", "default")},
				{ID: "cm2", Object: newConfigMap("cm2", "default")},
			},
			wantApplied:  2,
			wantNoErrors: true,
		},
		"skip apply resource excluded": {
			resources: []Resource{
				{ID: "cm1", Object: newConfigMap("cm1", "default")},
				{ID: "cm2", Object: newConfigMap("cm2", "default"), SkipApply: true},
			},
			wantApplied:  1,
			wantNoErrors: true,
		},
		"all skip apply": {
			resources: []Resource{
				{ID: "cm1", Object: newConfigMap("cm1", "default"), SkipApply: true},
			},
			wantApplied:  0,
			wantNoErrors: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			fakeClient := newFakeClient()

			applier := New(Config{
				Client:          fakeClient,
				RESTMapper:      mapper,
				Log:             logr.Discard(),
				ParentNamespace: "default",
			}, parent)

			result, _, err := applier.Apply(ctx, tt.resources, ApplyMode{})
			if err != nil {
				t.Fatalf("Apply() error = %v", err)
			}

			if len(result.Applied) != tt.wantApplied {
				t.Errorf("Apply() applied %d resources, want %d", len(result.Applied), tt.wantApplied)
			}

			if tt.wantNoErrors && result.Errors() != nil {
				t.Errorf("Apply() had errors: %v", result.Errors())
			}
		})
	}
}

func TestApply_MembershipLabels(t *testing.T) {
	ctx := context.Background()
	mapper := newTestRESTMapper()
	parent := newTestParent(schema.GroupVersionKind{
		Group: "delivery.ocm.software", Version: "v1alpha1", Kind: "TestKind",
	})
	fakeClient := newFakeClient()

	applier := New(Config{
		Client:          fakeClient,
		RESTMapper:      mapper,
		Log:             logr.Discard(),
		ParentNamespace: "default",
	}, parent)

	resources := []Resource{
		{ID: "cm1", Object: newConfigMap("cm1", "default")},
	}

	result, _, err := applier.Apply(ctx, resources, ApplyMode{})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if len(result.Applied) != 1 {
		t.Fatalf("Apply() applied %d resources, want 1", len(result.Applied))
	}

	item := result.Applied[0]
	if item.Error != nil {
		t.Fatalf("Apply() item error = %v", item.Error)
	}

	if item.Observed == nil {
		t.Fatal("Apply() returned nil Observed")
	}

	// Check that the applied object has the membership label
	labels := item.Observed.GetLabels()
	expectedID := ID(parent)
	if got := labels[ApplysetPartOfLabel]; got != expectedID {
		t.Errorf("Applied object label %s = %q, want %q", ApplysetPartOfLabel, got, expectedID)
	}
}

func TestApply_ChangeDetection(t *testing.T) {
	ctx := context.Background()
	mapper := newTestRESTMapper()
	parent := newTestParent(schema.GroupVersionKind{
		Group: "delivery.ocm.software", Version: "v1alpha1", Kind: "TestKind",
	})

	tests := map[string]struct {
		currentRevision string
		wantChanged     bool
	}{
		"no current revision - always changed": {
			currentRevision: "",
			wantChanged:     true,
		},
		"different revision - changed": {
			currentRevision: "old-rv",
			wantChanged:     true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			fakeClient := newFakeClient()

			applier := New(Config{
				Client:          fakeClient,
				RESTMapper:      mapper,
				Log:             logr.Discard(),
				ParentNamespace: "default",
			}, parent)

			resources := []Resource{
				{ID: "cm1", Object: newConfigMap("cm1", "default"), CurrentRevision: tt.currentRevision},
			}

			result, _, err := applier.Apply(ctx, resources, ApplyMode{})
			if err != nil {
				t.Fatalf("Apply() error = %v", err)
			}

			if len(result.Applied) != 1 {
				t.Fatalf("Apply() applied %d resources, want 1", len(result.Applied))
			}

			if result.Applied[0].Changed != tt.wantChanged {
				t.Errorf("Apply().Applied[0].Changed = %v, want %v", result.Applied[0].Changed, tt.wantChanged)
			}
		})
	}
}

func TestPrune(t *testing.T) {
	ctx := context.Background()
	mapper := newTestRESTMapper()

	// Create parent with annotations (required for prune scope)
	parent := &testParent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-instance",
			Namespace: "default",
			UID:       types.UID("test-parent-uid"),
			Annotations: map[string]string{
				ApplySetGKsAnnotation:                  "ConfigMap",
				ApplySetAdditionalNamespacesAnnotation: "default",
			},
		},
		gvk: schema.GroupVersionKind{Group: "delivery.ocm.software", Version: "v1alpha1", Kind: "TestKind"},
	}
	applySetID := ID(parent)

	// Create an orphan ConfigMap with the applyset label
	orphan := newConfigMap("orphan-cm", "default")
	orphan.SetLabels(map[string]string{
		ApplysetPartOfLabel: applySetID,
	})
	orphan.SetUID(types.UID("orphan-uid"))

	tests := map[string]struct {
		existingObjs []client.Object
		resources    []Resource
		callPrune    bool
		wantPruned   int
	}{
		"orphan gets pruned": {
			existingObjs: []client.Object{orphan},
			resources: []Resource{
				{ID: "cm1", Object: newConfigMap("cm1", "default")},
			},
			callPrune:  true,
			wantPruned: 1,
		},
		"prune not called": {
			existingObjs: []client.Object{orphan},
			resources: []Resource{
				{ID: "cm1", Object: newConfigMap("cm1", "default")},
			},
			callPrune:  false,
			wantPruned: 0,
		},
		"no orphans": {
			existingObjs: []client.Object{},
			resources: []Resource{
				{ID: "cm1", Object: newConfigMap("cm1", "default")},
			},
			callPrune:  true,
			wantPruned: 0,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			fakeClient := newFakeClient(tt.existingObjs...)

			applier := New(Config{
				Client:          fakeClient,
				RESTMapper:      mapper,
				Log:             logr.Discard(),
				ParentNamespace: "default",
			}, parent)

			result, _, err := applier.Apply(ctx, tt.resources, ApplyMode{})
			if err != nil {
				t.Fatalf("Apply() error = %v", err)
			}

			if !tt.callPrune {
				return
			}

			projectMeta, err := applier.Project(tt.resources)
			if err != nil {
				t.Fatalf("Project() error = %v", err)
			}
			pruneResult, err := applier.Prune(ctx, PruneOptions{
				KeepUIDs: result.ObservedUIDs(),
				Scope:    projectMeta.PruneScope(),
			})
			if err != nil {
				t.Fatalf("Prune() error = %v", err)
			}

			if len(pruneResult.Pruned) != tt.wantPruned {
				t.Errorf("Prune() pruned %d resources, want %d", len(pruneResult.Pruned), tt.wantPruned)
			}
		})
	}
}

func TestProject(t *testing.T) {
	mapper := newTestRESTMapper()
	parent := newTestParent(schema.GroupVersionKind{
		Group: "delivery.ocm.software", Version: "v1alpha1", Kind: "TestKind",
	})
	fakeClient := newFakeClient()

	applier := New(Config{
		Client:          fakeClient,
		RESTMapper:      mapper,
		Log:             logr.Discard(),
		ParentNamespace: "default",
	}, parent)

	tests := map[string]struct {
		resources   []Resource
		wantGKCount int
		wantNSCount int // AdditionalNamespaces excludes parent namespace
		wantHasGK   schema.GroupKind
		wantHasNS   string // empty means no additional namespaces expected
	}{
		"single configmap in parent namespace": {
			resources: []Resource{
				{ID: "cm1", Object: newConfigMap("cm1", "default")},
			},
			wantGKCount: 1,
			wantNSCount: 0, // parent ns excluded
			wantHasGK:   schema.GroupKind{Group: "", Kind: "ConfigMap"},
			wantHasNS:   "",
		},
		"multiple types in parent namespace": {
			resources: []Resource{
				{ID: "cm1", Object: newConfigMap("cm1", "default")},
				{ID: "secret1", Object: newSecret("secret1", "default")},
			},
			wantGKCount: 2,
			wantNSCount: 0, // parent ns excluded
			wantHasGK:   schema.GroupKind{Group: "", Kind: "Secret"},
			wantHasNS:   "",
		},
		"resources in additional namespace": {
			resources: []Resource{
				{ID: "cm1", Object: newConfigMap("cm1", "default")},
				{ID: "cm2", Object: newConfigMap("cm2", "other-ns")},
			},
			wantGKCount: 1,
			wantNSCount: 1, // only other-ns, parent excluded
			wantHasGK:   schema.GroupKind{Group: "", Kind: "ConfigMap"},
			wantHasNS:   "other-ns",
		},
		"skip apply excluded": {
			resources: []Resource{
				{ID: "cm1", Object: newConfigMap("cm1", "default")},
				{ID: "cm2", Object: newConfigMap("cm2", "other-ns"), SkipApply: true},
			},
			wantGKCount: 1,
			wantNSCount: 0, // skipped resource ns not counted, parent ns excluded
			wantHasGK:   schema.GroupKind{Group: "", Kind: "ConfigMap"},
			wantHasNS:   "",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			metadata, err := applier.Project(tt.resources)
			if err != nil {
				t.Fatalf("Project() error = %v", err)
			}

			if metadata.GroupKinds.Len() != tt.wantGKCount {
				t.Errorf("Project().GroupKinds has %d items, want %d", metadata.GroupKinds.Len(), tt.wantGKCount)
			}

			if !metadata.GroupKinds.Has(tt.wantHasGK) {
				t.Errorf("Project().GroupKinds missing %v", tt.wantHasGK)
			}

			if metadata.AdditionalNamespaces.Len() != tt.wantNSCount {
				t.Errorf("Project().AdditionalNamespaces has %d items, want %d", metadata.AdditionalNamespaces.Len(), tt.wantNSCount)
			}

			if tt.wantHasNS != "" && !metadata.AdditionalNamespaces.Has(tt.wantHasNS) {
				t.Errorf("Project().AdditionalNamespaces missing %q", tt.wantHasNS)
			}

			expectedID := ID(parent)
			if metadata.ID != expectedID {
				t.Errorf("Project().ID = %q, want %q", metadata.ID, expectedID)
			}
		})
	}
}

func TestPrune_ParentAnnotationsContributeToPruneScope(t *testing.T) {
	ctx := context.Background()
	mapper := newTestRESTMapper()

	// Create parent with existing annotations from a previous reconcile
	parent := &testParent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-instance",
			Namespace: "default",
			UID:       types.UID("test-parent-uid"),
			Annotations: map[string]string{
				ApplySetGKsAnnotation:                  "ConfigMap,Secret",
				ApplySetAdditionalNamespacesAnnotation: "default,old-ns",
			},
		},
		gvk: schema.GroupVersionKind{Group: "delivery.ocm.software", Version: "v1alpha1", Kind: "TestKind"},
	}
	applySetID := ID(parent)

	// Create an orphan Secret in old-ns
	orphanSecret := newSecret("orphan-secret", "old-ns")
	orphanSecret.SetLabels(map[string]string{
		ApplysetPartOfLabel: applySetID,
	})
	orphanSecret.SetUID(types.UID("orphan-secret-uid"))

	fakeClient := newFakeClient(orphanSecret)

	applier := New(Config{
		Client:          fakeClient,
		RESTMapper:      mapper,
		Log:             logr.Discard(),
		ParentNamespace: "default",
	}, parent)

	// Current reconcile only has ConfigMap in default namespace
	resources := []Resource{
		{ID: "cm1", Object: newConfigMap("cm1", "default")},
	}

	// Apply first to get UIDs
	result, batchMeta, err := applier.Apply(ctx, resources, ApplyMode{})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	// Apply returns batch-only metadata
	if batchMeta.GroupKinds.Len() != 1 {
		t.Errorf("Apply() batchMeta.GroupKinds has %d items, want 1 (batch only)", batchMeta.GroupKinds.Len())
	}

	// Prune with scope from Project()
	projectMeta, err := applier.Project(resources)
	if err != nil {
		t.Fatalf("Project() error = %v", err)
	}
	pruneResult, err := applier.Prune(ctx, PruneOptions{
		KeepUIDs: result.ObservedUIDs(),
		Scope:    projectMeta.PruneScope(),
	})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}

	// The orphan Secret should be pruned
	if len(pruneResult.Pruned) != 1 {
		t.Errorf("Prune() pruned %d resources, want 1", len(pruneResult.Pruned))
	}
}

func TestApply_ReturnsBatchOnlyMetadata(t *testing.T) {
	ctx := context.Background()
	mapper := newTestRESTMapper()

	// Parent with existing annotations
	parent := &testParent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-instance",
			Namespace: "default",
			UID:       types.UID("test-parent-uid"),
			Annotations: map[string]string{
				ApplySetGKsAnnotation:                  "ConfigMap,Secret",
				ApplySetAdditionalNamespacesAnnotation: "default,old-ns",
			},
		},
		gvk: schema.GroupVersionKind{Group: "delivery.ocm.software", Version: "v1alpha1", Kind: "TestKind"},
	}

	fakeClient := newFakeClient()

	applier := New(Config{
		Client:          fakeClient,
		RESTMapper:      mapper,
		Log:             logr.Discard(),
		ParentNamespace: "default",
	}, parent)

	resources := []Resource{
		{ID: "cm1", Object: newConfigMap("cm1", "default")},
	}

	_, batchMeta, err := applier.Apply(ctx, resources, ApplyMode{})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	// Batch metadata should only have current batch GKs
	if batchMeta.GroupKinds.Len() != 1 {
		t.Errorf("Apply() batchMeta.GroupKinds has %d items, want 1", batchMeta.GroupKinds.Len())
	}
	if !batchMeta.GroupKinds.Has(schema.GroupKind{Kind: "ConfigMap"}) {
		t.Error("batchMeta.GroupKinds should have ConfigMap")
	}

	// Project() returns union
	unionMeta, err := applier.Project(resources)
	if err != nil {
		t.Fatalf("Project() error = %v", err)
	}
	if unionMeta.GroupKinds.Len() != 2 {
		t.Errorf("Project() unionMeta.GroupKinds has %d items, want 2", unionMeta.GroupKinds.Len())
	}
}

func TestPrune_ClusterScopedResource(t *testing.T) {
	ctx := context.Background()
	mapper := newTestRESTMapper()
	parent := newTestParent(schema.GroupVersionKind{
		Group: "delivery.ocm.software", Version: "v1alpha1", Kind: "TestKind",
	})
	applySetID := ID(parent)

	orphanNS := newNamespace("orphan-ns")
	orphanNS.SetLabels(map[string]string{
		ApplysetPartOfLabel: applySetID,
	})
	orphanNS.SetUID("orphan-ns-uid")

	fakeClient := newFakeClient(orphanNS)

	parentWithAnnotations := &testParent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-instance",
			Namespace: "default",
			UID:       types.UID("test-parent-uid"),
			Annotations: map[string]string{
				ApplySetGKsAnnotation:                  "ConfigMap,Namespace",
				ApplySetAdditionalNamespacesAnnotation: "default",
			},
		},
		gvk: schema.GroupVersionKind{Group: "delivery.ocm.software", Version: "v1alpha1", Kind: "TestKind"},
	}

	applier := New(Config{
		Client:          fakeClient,
		RESTMapper:      mapper,
		Log:             logr.Discard(),
		ParentNamespace: "default",
	}, parentWithAnnotations)

	resources := []Resource{
		{ID: "cm1", Object: newConfigMap("cm1", "default")},
	}

	result, _, err := applier.Apply(ctx, resources, ApplyMode{})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	projectMeta, err := applier.Project(resources)
	if err != nil {
		t.Fatalf("Project() error = %v", err)
	}
	pruneResult, err := applier.Prune(ctx, PruneOptions{
		KeepUIDs: result.ObservedUIDs(),
		Scope:    projectMeta.PruneScope(),
	})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}

	if len(pruneResult.Pruned) != 1 {
		t.Errorf("Prune() pruned %d resources, want 1", len(pruneResult.Pruned))
	}
}

// mockParent implements metav1.Object and schema.ObjectKind for testing ID().
type mockParent struct {
	name      string
	namespace string
	gvk       schema.GroupVersionKind
}

func (m *mockParent) GetName() string                           { return m.name }
func (m *mockParent) GetNamespace() string                      { return m.namespace }
func (m *mockParent) GroupVersionKind() schema.GroupVersionKind { return m.gvk }
func (m *mockParent) SetGroupVersionKind(gvk schema.GroupVersionKind) {
	m.gvk = gvk
}

// Implement remaining metav1.Object methods
func (m *mockParent) GetGenerateName() string                       { return "" }
func (m *mockParent) SetGenerateName(name string)                   {}
func (m *mockParent) GetUID() types.UID                             { return "" }
func (m *mockParent) SetUID(uid types.UID)                          {}
func (m *mockParent) GetResourceVersion() string                    { return "" }
func (m *mockParent) SetResourceVersion(version string)             {}
func (m *mockParent) GetGeneration() int64                          { return 0 }
func (m *mockParent) SetGeneration(generation int64)                {}
func (m *mockParent) GetSelfLink() string                           { return "" }
func (m *mockParent) SetSelfLink(selfLink string)                   {}
func (m *mockParent) GetCreationTimestamp() metav1.Time             { return metav1.Time{} }
func (m *mockParent) SetCreationTimestamp(timestamp metav1.Time)    {}
func (m *mockParent) GetDeletionTimestamp() *metav1.Time            { return nil }
func (m *mockParent) SetDeletionTimestamp(timestamp *metav1.Time)   {}
func (m *mockParent) GetDeletionGracePeriodSeconds() *int64         { return nil }
func (m *mockParent) SetDeletionGracePeriodSeconds(s *int64)        {}
func (m *mockParent) GetLabels() map[string]string                  { return nil }
func (m *mockParent) SetLabels(labels map[string]string)            {}
func (m *mockParent) GetAnnotations() map[string]string             { return nil }
func (m *mockParent) SetAnnotations(annotations map[string]string)  {}
func (m *mockParent) GetFinalizers() []string                       { return nil }
func (m *mockParent) SetFinalizers(finalizers []string)             {}
func (m *mockParent) GetOwnerReferences() []metav1.OwnerReference   { return nil }
func (m *mockParent) SetOwnerReferences(r []metav1.OwnerReference)  {}
func (m *mockParent) GetManagedFields() []metav1.ManagedFieldsEntry { return nil }
func (m *mockParent) SetManagedFields(m2 []metav1.ManagedFieldsEntry) {
}
func (m *mockParent) SetName(name string)           { m.name = name }
func (m *mockParent) SetNamespace(namespace string) { m.namespace = namespace }

var applySetIDPattern = regexp.MustCompile(`^applyset-[A-Za-z0-9_-]+-v1$`)

func TestID(t *testing.T) {
	tests := map[string]struct {
		name      string
		namespace string
		kind      string
		group     string
	}{
		"basic namespaced resource": {
			name:      "myapp",
			namespace: "default",
			kind:      "MyKind",
			group:     "delivery.ocm.software",
		},
		"cluster-scoped resource": {
			name:      "myapp",
			namespace: "",
			kind:      "MyKind",
			group:     "delivery.ocm.software",
		},
		"core group": {
			name:      "myapp",
			namespace: "default",
			kind:      "ConfigMap",
			group:     "",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			parent := &mockParent{
				name:      tt.name,
				namespace: tt.namespace,
				gvk:       schema.GroupVersionKind{Group: tt.group, Version: "v1", Kind: tt.kind},
			}

			got := ID(parent)

			if !applySetIDPattern.MatchString(got) {
				t.Errorf("ID() = %q does not match pattern %q", got, applySetIDPattern.String())
			}
		})
	}
}

func TestID_Deterministic(t *testing.T) {
	parent := &mockParent{
		name:      "myapp",
		namespace: "default",
		gvk:       schema.GroupVersionKind{Group: "delivery.ocm.software", Version: "v1", Kind: "MyKind"},
	}

	id1 := ID(parent)
	id2 := ID(parent)

	if id1 != id2 {
		t.Errorf("ID() not deterministic: got %q and %q for same input", id1, id2)
	}
}

func TestID_DifferentNames(t *testing.T) {
	parent1 := &mockParent{
		name:      "app1",
		namespace: "default",
		gvk:       schema.GroupVersionKind{Group: "delivery.ocm.software", Version: "v1", Kind: "MyKind"},
	}
	parent2 := &mockParent{
		name:      "app2",
		namespace: "default",
		gvk:       schema.GroupVersionKind{Group: "delivery.ocm.software", Version: "v1", Kind: "MyKind"},
	}

	id1 := ID(parent1)
	id2 := ID(parent2)

	if id1 == id2 {
		t.Errorf("Different names should produce different IDs: both got %q", id1)
	}
}

func TestID_DifferentNamespaces(t *testing.T) {
	parent1 := &mockParent{
		name:      "myapp",
		namespace: "ns1",
		gvk:       schema.GroupVersionKind{Group: "delivery.ocm.software", Version: "v1", Kind: "MyKind"},
	}
	parent2 := &mockParent{
		name:      "myapp",
		namespace: "ns2",
		gvk:       schema.GroupVersionKind{Group: "delivery.ocm.software", Version: "v1", Kind: "MyKind"},
	}

	id1 := ID(parent1)
	id2 := ID(parent2)

	if id1 == id2 {
		t.Errorf("Different namespaces should produce different IDs: both got %q", id1)
	}
}

func TestID_DifferentKinds(t *testing.T) {
	parent1 := &mockParent{
		name:      "myapp",
		namespace: "default",
		gvk:       schema.GroupVersionKind{Group: "delivery.ocm.software", Version: "v1", Kind: "Kind1"},
	}
	parent2 := &mockParent{
		name:      "myapp",
		namespace: "default",
		gvk:       schema.GroupVersionKind{Group: "delivery.ocm.software", Version: "v1", Kind: "Kind2"},
	}

	id1 := ID(parent1)
	id2 := ID(parent2)

	if id1 == id2 {
		t.Errorf("Different kinds should produce different IDs: both got %q", id1)
	}
}

func TestID_DifferentGroups(t *testing.T) {
	parent1 := &mockParent{
		name:      "myapp",
		namespace: "default",
		gvk:       schema.GroupVersionKind{Group: "group1.io", Version: "v1", Kind: "MyKind"},
	}
	parent2 := &mockParent{
		name:      "myapp",
		namespace: "default",
		gvk:       schema.GroupVersionKind{Group: "group2.io", Version: "v1", Kind: "MyKind"},
	}

	id1 := ID(parent1)
	id2 := ID(parent2)

	if id1 == id2 {
		t.Errorf("Different groups should produce different IDs: both got %q", id1)
	}
}

func TestID_ClusterVsNamespacedDiffer(t *testing.T) {
	clustered := &mockParent{
		name:      "myapp",
		namespace: "",
		gvk:       schema.GroupVersionKind{Group: "delivery.ocm.software", Version: "v1", Kind: "MyKind"},
	}
	namespaced := &mockParent{
		name:      "myapp",
		namespace: "default",
		gvk:       schema.GroupVersionKind{Group: "delivery.ocm.software", Version: "v1", Kind: "MyKind"},
	}

	idCluster := ID(clustered)
	idNamespaced := ID(namespaced)

	if idCluster == idNamespaced {
		t.Errorf("Cluster-scoped and namespaced resources should have different IDs: both got %q", idCluster)
	}
}

func TestMetadata_GroupKindsString(t *testing.T) {
	tests := map[string]struct {
		gks  sets.Set[schema.GroupKind]
		want string
	}{
		"single core resource": {
			gks:  sets.New(schema.GroupKind{Group: "", Kind: "ConfigMap"}),
			want: "ConfigMap",
		},
		"single apps resource": {
			gks:  sets.New(schema.GroupKind{Group: "apps", Kind: "Deployment"}),
			want: "Deployment.apps",
		},
		"multiple sorted": {
			gks: sets.New(
				schema.GroupKind{Group: "", Kind: "Secret"},
				schema.GroupKind{Group: "apps", Kind: "Deployment"},
				schema.GroupKind{Group: "", Kind: "ConfigMap"},
			),
			want: "ConfigMap,Deployment.apps,Secret",
		},
		"empty set": {
			gks:  sets.New[schema.GroupKind](),
			want: "",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			m := Metadata{GroupKinds: tt.gks}
			got := m.GroupKindsString()
			if got != tt.want {
				t.Errorf("GroupKindsString() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMetadata_NamespacesString(t *testing.T) {
	tests := map[string]struct {
		namespaces sets.Set[string]
		want       string
	}{
		"single namespace": {
			namespaces: sets.New("default"),
			want:       "default",
		},
		"multiple sorted": {
			namespaces: sets.New("kube-system", "default", "ns1"),
			want:       "default,kube-system,ns1",
		},
		"empty set": {
			namespaces: sets.New[string](),
			want:       "",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			m := Metadata{AdditionalNamespaces: tt.namespaces}
			got := m.NamespacesString()
			if got != tt.want {
				t.Errorf("NamespacesString() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMetadata_Labels(t *testing.T) {
	m := Metadata{ID: "applyset-abc123-v1"}
	labels := m.Labels()

	if len(labels) != 1 {
		t.Errorf("Labels() returned %d labels, want 1", len(labels))
	}

	if got := labels[ApplySetParentIDLabel]; got != "applyset-abc123-v1" {
		t.Errorf("Labels()[%s] = %q, want %q", ApplySetParentIDLabel, got, "applyset-abc123-v1")
	}
}

func TestMetadata_Annotations(t *testing.T) {
	m := Metadata{
		ID:                   "applyset-abc123-v1",
		Tooling:              "delivery.ocm.software/v1.0.0",
		GroupKinds:           sets.New(schema.GroupKind{Kind: "ConfigMap"}),
		AdditionalNamespaces: sets.New("default"),
	}

	annotations := m.Annotations()

	if len(annotations) != 3 {
		t.Errorf("Annotations() returned %d annotations, want 3", len(annotations))
	}

	if got := annotations[ApplySetToolingAnnotation]; got != "delivery.ocm.software/v1.0.0" {
		t.Errorf("Annotations()[%s] = %q, want %q", ApplySetToolingAnnotation, got, "delivery.ocm.software/v1.0.0")
	}

	if got := annotations[ApplySetGKsAnnotation]; got != "ConfigMap" {
		t.Errorf("Annotations()[%s] = %q, want %q", ApplySetGKsAnnotation, got, "ConfigMap")
	}

	if got := annotations[ApplySetAdditionalNamespacesAnnotation]; got != "default" {
		t.Errorf("Annotations()[%s] = %q, want %q", ApplySetAdditionalNamespacesAnnotation, got, "default")
	}
}
