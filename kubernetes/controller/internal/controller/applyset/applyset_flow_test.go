package applyset

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"
	clientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// combinedFakeClient implements both client.Client and dynamic.Interface
type combinedFakeClient struct {
	client.Client
	*mockDynamicClient
}

type mockDynamicClient struct {
	dynamic.Interface
	mu      sync.RWMutex
	objects map[string]*unstructured.Unstructured
}

func (m *mockDynamicClient) Resource(resource schema.GroupVersionResource) dynamic.NamespaceableResourceInterface {
	return &mockResource{
		m:        m,
		resource: resource,
	}
}

type mockResource struct {
	m         *mockDynamicClient
	resource  schema.GroupVersionResource
	namespace string
}

func (r *mockResource) Namespace(ns string) dynamic.ResourceInterface {
	r.namespace = ns
	return r
}

func (r *mockResource) Create(ctx context.Context, obj *unstructured.Unstructured, opts metav1.CreateOptions, subresources ...string) (*unstructured.Unstructured, error) {
	key := r.key(obj.GetName())
	r.m.mu.Lock()
	r.m.objects[key] = obj
	r.m.mu.Unlock()
	return obj, nil
}

func (r *mockResource) Apply(ctx context.Context, name string, obj *unstructured.Unstructured, opts metav1.ApplyOptions, subresources ...string) (*unstructured.Unstructured, error) {
	if name == "fail-cm" {
		return nil, fmt.Errorf("injected failure")
	}
	if opts.DryRun != nil && len(opts.DryRun) > 0 && opts.DryRun[0] == metav1.DryRunAll {
		return obj, nil
	}
	key := r.key(name)
	r.m.mu.Lock()
	r.m.objects[key] = obj
	r.m.mu.Unlock()
	return obj, nil
}

func (r *mockResource) Get(ctx context.Context, name string, opts metav1.GetOptions, subresources ...string) (*unstructured.Unstructured, error) {
	key := r.key(name)
	r.m.mu.RLock()
	obj, ok := r.m.objects[key]
	r.m.mu.RUnlock()
	if !ok {
		return nil, apierrors.NewNotFound(r.resource.GroupResource(), name)
	}
	return obj, nil
}

func (r *mockResource) Delete(ctx context.Context, name string, opts metav1.DeleteOptions, subresources ...string) error {
	key := r.key(name)
	r.m.mu.Lock()
	delete(r.m.objects, key)
	r.m.mu.Unlock()
	return nil
}

func (r *mockResource) List(ctx context.Context, opts metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	list := &unstructured.UnstructuredList{}
	r.m.mu.RLock()
	defer r.m.mu.RUnlock()
	for _, obj := range r.m.objects {
		// Matching for the mock
		if opts.LabelSelector != "" {
			parts := strings.Split(opts.LabelSelector, "=")
			if len(parts) == 2 {
				if obj.GetLabels()[parts[0]] != parts[1] {
					continue
				}
			}
		}
		list.Items = append(list.Items, *obj)
	}
	return list, nil
}

func (r *mockResource) Update(ctx context.Context, obj *unstructured.Unstructured, opts metav1.UpdateOptions, subresources ...string) (*unstructured.Unstructured, error) {
	key := r.key(obj.GetName())
	r.m.mu.Lock()
	r.m.objects[key] = obj
	r.m.mu.Unlock()
	return obj, nil
}
func (r *mockResource) UpdateStatus(ctx context.Context, obj *unstructured.Unstructured, opts metav1.UpdateOptions) (*unstructured.Unstructured, error) {
	return obj, nil
}
func (r *mockResource) DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error {
	return nil
}
func (r *mockResource) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	return nil, nil
}
func (r *mockResource) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, nil
}
func (r *mockResource) ApplyStatus(ctx context.Context, name string, obj *unstructured.Unstructured, opts metav1.ApplyOptions) (*unstructured.Unstructured, error) {
	return obj, nil
}
func (r *mockResource) GetLister() interface{} { return nil }

func (r *mockResource) key(name string) string {
	return r.resource.String() + "/" + r.namespace + "/" + name
}

func newTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	return s
}

func setupTest(t *testing.T) (context.Context, *combinedFakeClient, *meta.DefaultRESTMapper) {
	ctx := context.Background()
	scheme := newTestScheme()

	// Setup RESTMapper
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{{Group: "", Version: "v1"}, {Group: "apps", Version: "v1"}})
	mapper.Add(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}, meta.RESTScopeNamespace)
	mapper.Add(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Secret"}, meta.RESTScopeNamespace)
	mapper.Add(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}, meta.RESTScopeNamespace)
	mapper.Add(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Namespace"}, meta.RESTScopeRoot)

	// Setup Clients
	c := clientfake.NewClientBuilder().WithScheme(scheme).Build()

	mockDC := &mockDynamicClient{
		objects: make(map[string]*unstructured.Unstructured),
	}
	fc := &combinedFakeClient{
		Client:            c,
		mockDynamicClient: mockDC,
	}
	fc.Interface = mockDC

	return ctx, fc, mapper
}

func TestNew_Success(t *testing.T) {
	ctx, fc, mapper := setupTest(t)

	parent := &unstructured.Unstructured{}
	parent.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})
	parent.SetName("parent")
	parent.SetNamespace("default")

	// Create parent in both clients
	err := fc.Create(ctx, parent)
	require.NoError(t, err)
	gvr := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}
	_, err = fc.Interface.Resource(gvr).Namespace("default").Create(ctx, parent, metav1.CreateOptions{})
	require.NoError(t, err)

	config := Config{
		ToolingID:    ToolingID{Name: "test", Version: "v1"},
		FieldManager: "test-manager",
	}

	set, err := New(ctx, parent, fc, mapper, config)
	require.NoError(t, err)
	require.NotNil(t, set)
	assert.Equal(t, ComputeID(parent), set.ID())
}

func TestApplySet_Add(t *testing.T) {
	ctx, fc, mapper := setupTest(t)

	parent := &unstructured.Unstructured{}
	parent.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})
	parent.SetName("parent")
	parent.SetNamespace("default")
	_ = fc.Create(ctx, parent)
	gvr := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}
	_, _ = fc.Interface.Resource(gvr).Namespace("default").Create(ctx, parent, metav1.CreateOptions{})

	set, err := New(ctx, parent, fc, mapper, Config{
		ToolingID:    ToolingID{Name: "test", Version: "v1"},
		FieldManager: "test-manager",
	})
	require.NoError(t, err)

	t.Run("add non-existing object", func(t *testing.T) {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})
		obj.SetName("cm1")
		obj.SetNamespace("default")

		observed, err := set.Add(ctx, obj)
		assert.NoError(t, err)
		assert.Nil(t, observed)
	})

	t.Run("add existing object", func(t *testing.T) {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})
		obj.SetName("cm2")
		obj.SetNamespace("default")

		// Pre-create in dynamic client
		gvr := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}
		_, _ = fc.Interface.Resource(gvr).Namespace("default").Create(ctx, obj, metav1.CreateOptions{})

		observed, err := set.Add(ctx, obj)
		assert.NoError(t, err)
		assert.NotNil(t, observed)
		assert.Equal(t, "cm2", observed.GetName())
	})
}

func TestApplySet_Apply(t *testing.T) {
	ctx, fc, mapper := setupTest(t)

	parent := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      "parent",
				"namespace": "default",
			},
		},
	}
	_ = fc.Create(ctx, parent)
	gvr := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}
	_, _ = fc.Interface.Resource(gvr).Namespace("default").Create(ctx, parent, metav1.CreateOptions{})

	set, err := New(ctx, parent, fc, mapper, Config{
		ToolingID:    ToolingID{Name: "test", Version: "v1"},
		FieldManager: "test-manager",
	})
	require.NoError(t, err)

	obj1 := &unstructured.Unstructured{}
	obj1.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})
	obj1.SetName("cm1")
	obj1.SetNamespace("default")
	obj1.SetLabels(map[string]string{"foo": "bar"})

	_, _ = set.Add(ctx, obj1)

	result, err := set.Apply(ctx, false)
	assert.NoError(t, err)
	assert.Len(t, result.Applied, 1)
	assert.Len(t, result.Errors, 0)

	// Verify object was applied with correct labels
	gvr = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}
	applied, err := fc.Interface.Resource(gvr).Namespace("default").Get(ctx, "cm1", metav1.GetOptions{})
	assert.NoError(t, err)
	assert.Equal(t, "bar", applied.GetLabels()["foo"])
	assert.Equal(t, set.ID(), applied.GetLabels()[ApplySetPartOfLabel])

	// Verify parent was updated
	parentUpdated, err := fc.mockDynamicClient.Resource(gvr).Namespace("default").Get(ctx, "parent", metav1.GetOptions{})
	assert.NoError(t, err)
	assert.Equal(t, set.ID(), parentUpdated.GetLabels()[ApplySetParentIDLabel])
	assert.Contains(t, parentUpdated.GetAnnotations()[ApplySetGKsAnnotation], "ConfigMap")
}

func TestApplySet_Prune(t *testing.T) {
	ctx, fc, mapper := setupTest(t)

	parent := &unstructured.Unstructured{}
	parent.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})
	parent.SetName("parent")
	parent.SetNamespace("default")

	// Create parent with initial state indicating it already manages a ConfigMap
	parent.SetAnnotations(map[string]string{
		ApplySetGKsAnnotation: "ConfigMap",
	})
	_ = fc.Create(ctx, parent)
	gvr := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}
	_, _ = fc.Interface.Resource(gvr).Namespace("default").Create(ctx, parent, metav1.CreateOptions{})

	set, err := New(ctx, parent, fc, mapper, Config{
		ToolingID:    ToolingID{Name: "test", Version: "v1"},
		FieldManager: "test-manager",
	})
	require.NoError(t, err)

	// Pre-create an object that should be pruned
	oldObj := &unstructured.Unstructured{}
	oldObj.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})
	oldObj.SetName("old-cm")
	oldObj.SetNamespace("default")
	oldObj.SetUID("old-uid")
	oldObj.SetLabels(map[string]string{
		ApplySetPartOfLabel: set.ID(),
	})
	gvr = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}
	_, _ = fc.Interface.Resource(gvr).Namespace("default").Create(ctx, oldObj, metav1.CreateOptions{})

	// Apply a new object
	newObj := &unstructured.Unstructured{}
	newObj.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})
	newObj.SetName("new-cm")
	newObj.SetNamespace("default")
	newObj.SetUID("new-uid")
	_, _ = set.Add(ctx, newObj)

	// Apply with prune=true
	result, err := set.Apply(ctx, true)
	assert.NoError(t, err)
	assert.Len(t, result.Applied, 1)
	assert.Len(t, result.Pruned, 1)
	assert.Equal(t, "old-cm", result.Pruned[0].Name)

	// Verify old object is gone
	_, err = fc.Interface.Resource(gvr).Namespace("default").Get(ctx, "old-cm", metav1.GetOptions{})
	assert.Error(t, err)

	// Verify new object exists
	_, err = fc.Interface.Resource(gvr).Namespace("default").Get(ctx, "new-cm", metav1.GetOptions{})
	assert.NoError(t, err)
}

func TestApplySet_DryRun(t *testing.T) {
	ctx, fc, mapper := setupTest(t)

	parent := &unstructured.Unstructured{}
	parent.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})
	parent.SetName("parent")
	parent.SetNamespace("default")
	_ = fc.Create(ctx, parent)
	gvr := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}
	_, _ = fc.Interface.Resource(gvr).Namespace("default").Create(ctx, parent, metav1.CreateOptions{})

	set, err := New(ctx, parent, fc, mapper, Config{
		ToolingID:    ToolingID{Name: "test", Version: "v1"},
		FieldManager: "test-manager",
	})
	require.NoError(t, err)

	obj1 := &unstructured.Unstructured{}
	obj1.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})
	obj1.SetName("cm1")
	obj1.SetNamespace("default")
	_, _ = set.Add(ctx, obj1)

	result, err := set.DryRun(ctx, false)
	assert.NoError(t, err)
	assert.Len(t, result.Applied, 1)

	// Verify object was NOT applied to cluster
	gvr = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}
	_, err = fc.Interface.Resource(gvr).Namespace("default").Get(ctx, "cm1", metav1.GetOptions{})
	assert.Error(t, err)
}

func TestApplySet_Concurrency(t *testing.T) {
	ctx, fc, mapper := setupTest(t)

	parent := &unstructured.Unstructured{}
	parent.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})
	parent.SetName("parent")
	parent.SetNamespace("default")
	_ = fc.Create(ctx, parent)
	gvr := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}
	_, _ = fc.Interface.Resource(gvr).Namespace("default").Create(ctx, parent, metav1.CreateOptions{})

	set, _ := New(ctx, parent, fc, mapper, Config{
		ToolingID:    ToolingID{Name: "test", Version: "v1"},
		FieldManager: "test-manager",
		Concurrency:  5,
	})

	for i := 0; i < 20; i++ {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})
		obj.SetName(fmt.Sprintf("cm-%d", i))
		obj.SetNamespace("default")
		obj.SetUID(types.UID(fmt.Sprintf("uid-%d", i)))
		_, _ = set.Add(ctx, obj)
	}

	result, err := set.Apply(ctx, false)
	assert.NoError(t, err)
	assert.Len(t, result.Applied, 20)
}

func TestApplySet_PartialErrors(t *testing.T) {
	ctx, fc, mapper := setupTest(t)

	parent := &unstructured.Unstructured{}
	parent.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})
	parent.SetName("parent")
	parent.SetNamespace("default")
	_ = fc.Create(ctx, parent)
	gvr := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}
	_, _ = fc.Interface.Resource(gvr).Namespace("default").Create(ctx, parent, metav1.CreateOptions{})

	set, _ := New(ctx, parent, fc, mapper, Config{
		ToolingID:    ToolingID{Name: "test", Version: "v1"},
		FieldManager: "test-manager",
	})

	// Add one valid object
	obj1 := &unstructured.Unstructured{}
	obj1.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})
	obj1.SetName("ok-cm")
	obj1.SetNamespace("default")
	obj1.SetUID("ok-uid")
	_, _ = set.Add(ctx, obj1)

	// Add another object that will fail (injected in mock.Apply)
	obj2 := &unstructured.Unstructured{}
	obj2.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})
	obj2.SetName("fail-cm")
	obj2.SetNamespace("default")
	obj2.SetUID("fail-uid")
	_, _ = set.Add(ctx, obj2)

	result, err := set.Apply(ctx, false)
	assert.NoError(t, err)
	assert.Len(t, result.Applied, 2)
	assert.Len(t, result.Errors, 1)
	assert.Contains(t, result.Errors[0].Error(), "injected failure")

	// Verify ok-cm was applied
	_, err = fc.Interface.Resource(gvr).Namespace("default").Get(ctx, "ok-cm", metav1.GetOptions{})
	assert.NoError(t, err)

	// Verify fail-cm was NOT applied
	_, err = fc.Interface.Resource(gvr).Namespace("default").Get(ctx, "fail-cm", metav1.GetOptions{})
	assert.Error(t, err)
}
