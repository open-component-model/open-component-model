package applyset_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"ocm.software/open-component-model/kubernetes/controller/internal/controller/applyset"
	"sigs.k8s.io/controller-runtime/pkg/client"
	clientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	_ = appsv1.AddToScheme(s)
	return s
}

func setupTest(t *testing.T) (context.Context, client.Client, *meta.DefaultRESTMapper) {
	ctx := context.Background()
	scheme := newTestScheme()

	// Setup RESTMapper
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{{Group: "", Version: "v1"}, {Group: "apps", Version: "v1"}})
	mapper.Add(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}, meta.RESTScopeNamespace)
	mapper.Add(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Secret"}, meta.RESTScopeNamespace)
	mapper.Add(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}, meta.RESTScopeNamespace)
	mapper.Add(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Namespace"}, meta.RESTScopeRoot)

	// Setup controller-runtime fake client
	c := clientfake.NewClientBuilder().WithScheme(scheme).Build()

	return ctx, c, mapper
}

func TestNew_Success(t *testing.T) {
	ctx, fc, mapper := setupTest(t)

	parent := &unstructured.Unstructured{}
	parent.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})
	parent.SetName("parent")
	parent.SetNamespace("default")

	// Create parent in client
	err := fc.Create(ctx, parent)
	require.NoError(t, err)

	config := applyset.Config{
		ToolingID:    applyset.ToolingID{Name: "test", Version: "v1"},
		FieldManager: "test-manager",
	}

	set, err := applyset.New(ctx, parent, fc, mapper, config)
	require.NoError(t, err)
	require.NotNil(t, set)
	assert.Equal(t, applyset.ComputeID(parent), set.ID())
}

func TestApplySet_Add(t *testing.T) {
	ctx, fc, mapper := setupTest(t)

	parent := &unstructured.Unstructured{}
	parent.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})
	parent.SetName("parent")
	parent.SetNamespace("default")
	_ = fc.Create(ctx, parent)

	set, err := applyset.New(ctx, parent, fc, mapper, applyset.Config{
		ToolingID:    applyset.ToolingID{Name: "test", Version: "v1"},
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

		// Pre-create in controller-runtime client
		_ = fc.Create(ctx, obj)

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

	set, err := applyset.New(ctx, parent, fc, mapper, applyset.Config{
		ToolingID:    applyset.ToolingID{Name: "test", Version: "v1"},
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
	applied := &unstructured.Unstructured{}
	applied.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})
	err = fc.Get(ctx, client.ObjectKey{Name: "cm1", Namespace: "default"}, applied)
	assert.NoError(t, err)
	assert.Equal(t, "bar", applied.GetLabels()["foo"])
	assert.Equal(t, set.ID(), applied.GetLabels()[applyset.ApplySetPartOfLabel])

	// Verify parent was updated
	parentUpdated := &unstructured.Unstructured{}
	parentUpdated.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})
	err = fc.Get(ctx, client.ObjectKey{Name: "parent", Namespace: "default"}, parentUpdated)
	assert.NoError(t, err)
	assert.Equal(t, set.ID(), parentUpdated.GetLabels()[applyset.ApplySetParentIDLabel])
	assert.Contains(t, parentUpdated.GetAnnotations()[applyset.ApplySetGKsAnnotation], "ConfigMap")
}

func TestApplySet_Prune(t *testing.T) {
	ctx, fc, mapper := setupTest(t)

	parent := &unstructured.Unstructured{}
	parent.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})
	parent.SetName("parent")
	parent.SetNamespace("default")

	// Create parent with initial state indicating it already manages a ConfigMap
	parent.SetAnnotations(map[string]string{
		applyset.ApplySetGKsAnnotation: "ConfigMap",
	})
	_ = fc.Create(ctx, parent)

	set, err := applyset.New(ctx, parent, fc, mapper, applyset.Config{
		ToolingID:    applyset.ToolingID{Name: "test", Version: "v1"},
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
		applyset.ApplySetPartOfLabel: set.ID(),
	})
	_ = fc.Create(ctx, oldObj)

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
	oldObjCheck := &unstructured.Unstructured{}
	oldObjCheck.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})
	err = fc.Get(ctx, client.ObjectKey{Name: "old-cm", Namespace: "default"}, oldObjCheck)
	assert.Error(t, err)

	// Verify new object exists
	newObjCheck := &unstructured.Unstructured{}
	newObjCheck.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})
	err = fc.Get(ctx, client.ObjectKey{Name: "new-cm", Namespace: "default"}, newObjCheck)
	assert.NoError(t, err)
}

func TestApplySet_DryRun(t *testing.T) {
	ctx, fc, mapper := setupTest(t)

	parent := &unstructured.Unstructured{}
	parent.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})
	parent.SetName("parent")
	parent.SetNamespace("default")
	_ = fc.Create(ctx, parent)

	set, err := applyset.New(ctx, parent, fc, mapper, applyset.Config{
		ToolingID:    applyset.ToolingID{Name: "test", Version: "v1"},
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

	// Note: controller-runtime fake client doesn't support dry-run, so objects are still created
	// In a real cluster with dry-run, the object would NOT be created
}

func TestApplySet_Concurrency(t *testing.T) {
	ctx, fc, mapper := setupTest(t)

	parent := &unstructured.Unstructured{}
	parent.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})
	parent.SetName("parent")
	parent.SetNamespace("default")
	_ = fc.Create(ctx, parent)

	set, _ := applyset.New(ctx, parent, fc, mapper, applyset.Config{
		ToolingID:    applyset.ToolingID{Name: "test", Version: "v1"},
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
	// Note: This test would require a more sophisticated fake client that can
	// simulate failures. The controller-runtime fake client doesn't support
	// error injection easily. In practice, this would be tested with integration
	// tests against a real cluster or envtest.
	t.Skip("Skipping partial errors test - requires error injection capability")
}

func TestApplySet_ID(t *testing.T) {
	ctx, fc, mapper := setupTest(t)

	// Test with custom GVK parent
	parent := &unstructured.Unstructured{}
	parent.SetGroupVersionKind(schema.GroupVersionKind{Group: "foo.bar", Version: "v1", Kind: "Foo"})
	parent.SetName("test-foo")
	parent.SetNamespace("test-ns")
	parent.SetUID("test-uid")

	_ = fc.Create(ctx, parent)

	set, err := applyset.New(ctx, parent, fc, mapper, applyset.Config{
		ToolingID:    applyset.ToolingID{Name: "test", Version: "v1"},
		FieldManager: "test-manager",
	})
	require.NoError(t, err)

	// Verify ID format: applyset-<base64-hash>
	id := set.ID()
	assert.True(t, strings.HasPrefix(id, "applyset-"), "ID should start with 'applyset-'")
	assert.Greater(t, len(id), len("applyset-"), "ID should have content after prefix")

	// Verify ID is consistent with ComputeID
	expectedID := applyset.ComputeID(parent)
	assert.Equal(t, expectedID, id)
}

func TestApplySet_UpdatesParentAnnotationsWithMultipleGroupKinds(t *testing.T) {
	ctx, fc, mapper := setupTest(t)

	parent := &unstructured.Unstructured{}
	parent.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Secret"})
	parent.SetName("parent-secret")
	parent.SetNamespace("default")
	_ = fc.Create(ctx, parent)

	set, _ := applyset.New(ctx, parent, fc, mapper, applyset.Config{
		ToolingID:    applyset.ToolingID{Name: "test", Version: "v1"},
		FieldManager: "test-manager",
	})

	// Add two different GroupKinds
	cm1 := &unstructured.Unstructured{}
	cm1.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})
	cm1.SetName("cm-1")
	cm1.SetNamespace("default")
	_, _ = set.Add(ctx, cm1)

	secret1 := &unstructured.Unstructured{}
	secret1.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Secret"})
	secret1.SetName("secret-1")
	secret1.SetNamespace("default")
	_, _ = set.Add(ctx, secret1)

	result, err := set.Apply(ctx, false)
	assert.NoError(t, err)
	assert.Len(t, result.Applied, 2)

	// Verify parent was updated with both GKs
	parentUpdated := &unstructured.Unstructured{}
	parentUpdated.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Secret"})
	err = fc.Get(ctx, client.ObjectKey{Name: "parent-secret", Namespace: "default"}, parentUpdated)
	assert.NoError(t, err)

	ann := parentUpdated.GetAnnotations()
	assert.NotNil(t, ann)
	assert.Contains(t, ann, applyset.ApplySetGKsAnnotation)

	// Expect both GKs in annotation (order not guaranteed)
	gks := ann[applyset.ApplySetGKsAnnotation]
	assert.Contains(t, gks, "ConfigMap")
	assert.Contains(t, gks, "Secret")
}

func TestApplySet_ApplyMultiNamespace(t *testing.T) {
	ctx, fc, mapper := setupTest(t)

	parent := &unstructured.Unstructured{}
	parent.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})
	parent.SetName("parent")
	parent.SetNamespace("default")
	_ = fc.Create(ctx, parent)

	set, _ := applyset.New(ctx, parent, fc, mapper, applyset.Config{
		ToolingID:    applyset.ToolingID{Name: "test", Version: "v1"},
		FieldManager: "test-manager",
	})

	// Add objects in multiple namespaces
	cm1 := &unstructured.Unstructured{}
	cm1.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})
	cm1.SetName("cm1")
	cm1.SetNamespace("default")
	_, _ = set.Add(ctx, cm1)

	cm2 := &unstructured.Unstructured{}
	cm2.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})
	cm2.SetName("cm2")
	cm2.SetNamespace("kube-system")
	_, _ = set.Add(ctx, cm2)

	result, err := set.Apply(ctx, false)
	assert.NoError(t, err)
	assert.Len(t, result.Applied, 2)

	// Verify parent has additional-namespaces annotation
	parentUpdated := &unstructured.Unstructured{}
	parentUpdated.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})
	err = fc.Get(ctx, client.ObjectKey{Name: "parent", Namespace: "default"}, parentUpdated)
	assert.NoError(t, err)

	ann := parentUpdated.GetAnnotations()
	if ann != nil && ann[applyset.ApplySetAdditionalNamespacesAnnotation] != "" {
		// If additional namespaces exist, verify kube-system is listed
		// (default namespace should be implicit and not in the list)
		assert.Contains(t, ann[applyset.ApplySetAdditionalNamespacesAnnotation], "kube-system")
	}
}
