package applyset

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
)

func Test_ToolingID_String(t *testing.T) {
	tests := []struct {
		name     string
		tooling  ToolingID
		expected string
	}{
		{
			name:     "basic tooling ID",
			tooling:  ToolingID{Name: "kubectl", Version: "v1.28.0"},
			expected: "kubectl/v1.28.0",
		},
		{
			name:     "custom controller",
			tooling:  ToolingID{Name: "my-controller", Version: "v0.1.0"},
			expected: "my-controller/v0.1.0",
		},
		{
			name:     "empty version",
			tooling:  ToolingID{Name: "tool", Version: ""},
			expected: "tool/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.tooling.String()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func Test_Result_AppliedUIDs(t *testing.T) {
	tests := []struct {
		name     string
		result   *Result
		expected []types.UID
	}{
		{
			name: "no applied objects",
			result: &Result{
				Applied: []AppliedObject{},
			},
			expected: []types.UID{},
		},
		{
			name: "all successful",
			result: &Result{
				Applied: []AppliedObject{
					{
						Object: &unstructured.Unstructured{
							Object: map[string]interface{}{
								"metadata": map[string]interface{}{
									"uid": "uid-1",
								},
							},
						},
						Error: nil,
					},
					{
						Object: &unstructured.Unstructured{
							Object: map[string]interface{}{
								"metadata": map[string]interface{}{
									"uid": "uid-2",
								},
							},
						},
						Error: nil,
					},
				},
			},
			expected: []types.UID{"uid-1", "uid-2"},
		},
		{
			name: "mixed success and errors",
			result: &Result{
				Applied: []AppliedObject{
					{
						Object: &unstructured.Unstructured{
							Object: map[string]interface{}{
								"metadata": map[string]interface{}{
									"uid": "uid-1",
								},
							},
						},
						Error: nil,
					},
					{
						Object: &unstructured.Unstructured{
							Object: map[string]interface{}{
								"metadata": map[string]interface{}{
									"uid": "uid-2",
								},
							},
						},
						Error: assert.AnError,
					},
					{
						Object: &unstructured.Unstructured{
							Object: map[string]interface{}{
								"metadata": map[string]interface{}{
									"uid": "uid-3",
								},
							},
						},
						Error: nil,
					},
				},
			},
			expected: []types.UID{"uid-1", "uid-3"},
		},
		{
			name: "nil objects ignored",
			result: &Result{
				Applied: []AppliedObject{
					{
						Object: nil,
						Error:  assert.AnError,
					},
					{
						Object: &unstructured.Unstructured{
							Object: map[string]interface{}{
								"metadata": map[string]interface{}{
									"uid": "uid-1",
								},
							},
						},
						Error: nil,
					},
				},
			},
			expected: []types.UID{"uid-1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uids := tt.result.AppliedUIDs()
			assert.Equal(t, len(tt.expected), uids.Len())
			for _, uid := range tt.expected {
				assert.True(t, uids.Has(uid), "expected UID %s to be in result", uid)
			}
		})
	}
}

func Test_Result_RecordApplied(t *testing.T) {
	t.Run("successful apply", func(t *testing.T) {
		result := &Result{
			Applied: make([]AppliedObject, 0),
			Errors:  make([]error, 0),
		}

		obj := &unstructured.Unstructured{}
		obj.SetName("test-obj")

		result.recordApplied(obj, nil)

		assert.Len(t, result.Applied, 1)
		assert.Equal(t, "test-obj", result.Applied[0].Object.GetName())
		assert.Nil(t, result.Applied[0].Error)
		assert.Len(t, result.Errors, 0)
	})

	t.Run("failed apply", func(t *testing.T) {
		result := &Result{
			Applied: make([]AppliedObject, 0),
			Errors:  make([]error, 0),
		}

		obj := &unstructured.Unstructured{}
		obj.SetName("test-obj")
		err := assert.AnError

		result.recordApplied(obj, err)

		assert.Len(t, result.Applied, 1)
		assert.Equal(t, "test-obj", result.Applied[0].Object.GetName())
		assert.Equal(t, err, result.Applied[0].Error)
		assert.Len(t, result.Errors, 1)
		assert.Equal(t, err, result.Errors[0])
	})

	t.Run("concurrent applies", func(t *testing.T) {
		result := &Result{
			Applied: make([]AppliedObject, 0),
			Errors:  make([]error, 0),
		}

		// Simulate concurrent recordApplied calls
		done := make(chan bool)
		for i := 0; i < 10; i++ {
			go func(id int) {
				obj := &unstructured.Unstructured{}
				obj.SetName("test-obj")
				result.recordApplied(obj, nil)
				done <- true
			}(i)
		}

		// Wait for all goroutines
		for i := 0; i < 10; i++ {
			<-done
		}

		assert.Len(t, result.Applied, 10)
		assert.Len(t, result.Errors, 0)
	})
}

func Test_Result_RecordPruned(t *testing.T) {
	t.Run("successful prune", func(t *testing.T) {
		result := &Result{
			Pruned: make([]PrunedObject, 0),
			Errors: make([]error, 0),
		}

		prunable := PrunableObject{
			Name:      "test-obj",
			Namespace: "default",
			GVK:       schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"},
			UID:       "uid-123",
		}

		result.recordPruned(prunable, nil)

		assert.Len(t, result.Pruned, 1)
		assert.Equal(t, "test-obj", result.Pruned[0].Name)
		assert.Equal(t, "default", result.Pruned[0].Namespace)
		assert.Nil(t, result.Pruned[0].Error)
		assert.Len(t, result.Errors, 0)
	})

	t.Run("failed prune", func(t *testing.T) {
		result := &Result{
			Pruned: make([]PrunedObject, 0),
			Errors: make([]error, 0),
		}

		prunable := PrunableObject{
			Name:      "test-obj",
			Namespace: "default",
			GVK:       schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"},
			UID:       "uid-123",
		}
		err := assert.AnError

		result.recordPruned(prunable, err)

		assert.Len(t, result.Pruned, 1)
		assert.Equal(t, err, result.Pruned[0].Error)
		assert.Len(t, result.Errors, 1)
		assert.Equal(t, err, result.Errors[0])
	})
}

func Test_ApplySet_InjectApplySetLabels(t *testing.T) {
	parent := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-parent",
			Namespace: "default",
		},
	}
	parent.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "ConfigMap",
	})

	aset := &applySet{
		parent: parent,
	}

	tests := []struct {
		name           string
		inputLabels    map[string]string
		expectedLabels map[string]string
	}{
		{
			name:        "nil labels",
			inputLabels: nil,
			expectedLabels: map[string]string{
				ApplySetPartOfLabel: aset.ID(),
			},
		},
		{
			name:        "empty labels",
			inputLabels: map[string]string{},
			expectedLabels: map[string]string{
				ApplySetPartOfLabel: aset.ID(),
			},
		},
		{
			name: "existing labels preserved",
			inputLabels: map[string]string{
				"app":         "myapp",
				"environment": "prod",
			},
			expectedLabels: map[string]string{
				"app":               "myapp",
				"environment":       "prod",
				ApplySetPartOfLabel: aset.ID(),
			},
		},
		{
			name: "applyset label overwritten",
			inputLabels: map[string]string{
				ApplySetPartOfLabel: "wrong-id",
			},
			expectedLabels: map[string]string{
				ApplySetPartOfLabel: aset.ID(),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := aset.injectApplySetLabels(tt.inputLabels)
			assert.Equal(t, tt.expectedLabels, result)
		})
	}
}

func Test_ApplySet_InjectToolLabels(t *testing.T) {
	aset := &applySet{
		toolLabels: map[string]string{
			"tool.label/managed-by": "my-tool",
			"tool.label/version":    "v1.0",
		},
	}

	tests := []struct {
		name           string
		inputLabels    map[string]string
		expectedLabels map[string]string
	}{
		{
			name:        "nil labels",
			inputLabels: nil,
			expectedLabels: map[string]string{
				"tool.label/managed-by": "my-tool",
				"tool.label/version":    "v1.0",
			},
		},
		{
			name:        "empty labels",
			inputLabels: map[string]string{},
			expectedLabels: map[string]string{
				"tool.label/managed-by": "my-tool",
				"tool.label/version":    "v1.0",
			},
		},
		{
			name: "merge with existing labels",
			inputLabels: map[string]string{
				"app": "myapp",
			},
			expectedLabels: map[string]string{
				"app":                   "myapp",
				"tool.label/managed-by": "my-tool",
				"tool.label/version":    "v1.0",
			},
		},
		{
			name: "tool labels overwrite existing",
			inputLabels: map[string]string{
				"tool.label/managed-by": "other-tool",
			},
			expectedLabels: map[string]string{
				"tool.label/managed-by": "my-tool",
				"tool.label/version":    "v1.0",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := aset.injectToolLabels(tt.inputLabels)
			assert.Equal(t, tt.expectedLabels, result)
		})
	}
}

func Test_ApplySet_InjectToolLabels_NoToolLabels(t *testing.T) {
	aset := &applySet{
		toolLabels: nil,
	}

	inputLabels := map[string]string{
		"app": "myapp",
	}

	result := aset.injectToolLabels(inputLabels)
	assert.Equal(t, inputLabels, result)
}

func Test_ApplySet_DesiredParentLabels(t *testing.T) {
	parent := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-parent",
			Namespace: "default",
		},
	}
	parent.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "ConfigMap",
	})

	aset := &applySet{
		parent: parent,
	}

	labels := aset.desiredParentLabels()

	require.NotNil(t, labels)
	assert.Equal(t, aset.ID(), labels[ApplySetParentIDLabel])
	assert.Len(t, labels, 1)
}

func Test_ApplySet_DesiredParentAnnotations(t *testing.T) {
	parent := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-parent",
			Namespace: "default",
		},
	}
	parent.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "ConfigMap",
	})

	t.Run("without superset - empty current", func(t *testing.T) {
		aset := &applySet{
			parent:    parent,
			toolingID: ToolingID{Name: "test-tool", Version: "v1.0"},
			desiredRESTMappings: map[schema.GroupKind]*meta.RESTMapping{
				{Kind: "ConfigMap"}:                  nil,
				{Kind: "Deployment"}:                 nil,
				{Group: "apps", Kind: "StatefulSet"}: nil,
			},
			desiredNamespaces:  sets.New("default", "kube-system"),
			currentAnnotations: map[string]string{},
		}

		annotations, nss, gks := aset.desiredParentAnnotations(false)

		assert.Equal(t, "test-tool/v1.0", annotations[ApplySetToolingAnnotation])
		assert.Contains(t, annotations[ApplySetGKsAnnotation], "ConfigMap")
		assert.Contains(t, annotations[ApplySetGKsAnnotation], "Deployment")
		assert.Contains(t, annotations[ApplySetGKsAnnotation], "StatefulSet.apps")
		assert.Equal(t, "kube-system", annotations[ApplySetAdditionalNamespacesAnnotation])
		assert.Len(t, nss, 1)
		assert.Len(t, gks, 3)
	})

	t.Run("with superset - merge current", func(t *testing.T) {
		aset := &applySet{
			parent:    parent,
			toolingID: ToolingID{Name: "test-tool", Version: "v1.0"},
			desiredRESTMappings: map[schema.GroupKind]*meta.RESTMapping{
				{Kind: "ConfigMap"}: nil,
			},
			desiredNamespaces: sets.New("default"),
			currentAnnotations: map[string]string{
				ApplySetGKsAnnotation:                  "Secret,Pod",
				ApplySetAdditionalNamespacesAnnotation: "other-ns",
			},
		}

		annotations, nss, gks := aset.desiredParentAnnotations(true)

		assert.Contains(t, annotations[ApplySetGKsAnnotation], "ConfigMap")
		assert.Contains(t, annotations[ApplySetGKsAnnotation], "Secret")
		assert.Contains(t, annotations[ApplySetGKsAnnotation], "Pod")
		assert.Contains(t, annotations[ApplySetAdditionalNamespacesAnnotation], "other-ns")
		assert.True(t, nss.Has("other-ns"))
		assert.Len(t, gks, 3) // ConfigMap, Secret, Pod
	})

	t.Run("parent namespace excluded from additional", func(t *testing.T) {
		aset := &applySet{
			parent:    parent,
			toolingID: ToolingID{Name: "test-tool", Version: "v1.0"},
			desiredRESTMappings: map[schema.GroupKind]*meta.RESTMapping{
				{Kind: "ConfigMap"}: nil,
			},
			desiredNamespaces:  sets.New("default", "kube-system"),
			currentAnnotations: map[string]string{},
		}

		annotations, _, _ := aset.desiredParentAnnotations(false)

		// default is parent namespace, should not be in additional
		assert.Equal(t, "kube-system", annotations[ApplySetAdditionalNamespacesAnnotation])
	})
}

func Test_ApplySet_GetGKsFromAnnotations(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		expected    []string
	}{
		{
			name:        "no annotation",
			annotations: map[string]string{},
			expected:    nil,
		},
		{
			name: "single GK",
			annotations: map[string]string{
				ApplySetGKsAnnotation: "ConfigMap",
			},
			expected: []string{"ConfigMap"},
		},
		{
			name: "multiple GKs",
			annotations: map[string]string{
				ApplySetGKsAnnotation: "ConfigMap,Secret,Pod",
			},
			expected: []string{"ConfigMap", "Secret", "Pod"},
		},
		{
			name: "GKs with spaces",
			annotations: map[string]string{
				ApplySetGKsAnnotation: "ConfigMap, Secret , Pod",
			},
			expected: []string{"ConfigMap", "Secret", "Pod"},
		},
		{
			name: "empty GK filtered out",
			annotations: map[string]string{
				ApplySetGKsAnnotation: "ConfigMap,,Pod",
			},
			expected: []string{"ConfigMap", "Pod"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			aset := &applySet{
				currentAnnotations: tt.annotations,
			}

			result := aset.getGKsFromAnnotations()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func Test_ApplySet_GetNamespacesToCheck(t *testing.T) {
	t.Run("parent with namespace", func(t *testing.T) {
		parent := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-parent",
				Namespace: "default",
			},
		}

		aset := &applySet{
			parent: parent,
			currentAnnotations: map[string]string{
				ApplySetAdditionalNamespacesAnnotation: "kube-system,prod",
			},
		}

		namespaces := aset.getNamespacesToCheck()

		assert.True(t, namespaces.Has("default"))
		assert.True(t, namespaces.Has("kube-system"))
		assert.True(t, namespaces.Has("prod"))
		assert.Equal(t, 3, namespaces.Len())
	})

	t.Run("cluster-scoped parent", func(t *testing.T) {
		parent := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-namespace",
			},
		}

		aset := &applySet{
			parent: parent,
			currentAnnotations: map[string]string{
				ApplySetAdditionalNamespacesAnnotation: "kube-system,prod",
			},
		}

		namespaces := aset.getNamespacesToCheck()

		assert.False(t, namespaces.Has("test-namespace"))
		assert.True(t, namespaces.Has("kube-system"))
		assert.True(t, namespaces.Has("prod"))
		assert.Equal(t, 2, namespaces.Len())
	})

	t.Run("no additional namespaces", func(t *testing.T) {
		parent := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-parent",
				Namespace: "default",
			},
		}

		aset := &applySet{
			parent:             parent,
			currentAnnotations: map[string]string{},
		}

		namespaces := aset.getNamespacesToCheck()

		assert.True(t, namespaces.Has("default"))
		assert.Equal(t, 1, namespaces.Len())
	})
}

func Test_ParseGroupKind(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected schema.GroupKind
	}{
		{
			name:  "core API - no group",
			input: "ConfigMap",
			expected: schema.GroupKind{
				Kind: "ConfigMap",
			},
		},
		{
			name:  "with group",
			input: "Deployment.apps",
			expected: schema.GroupKind{
				Kind:  "Deployment",
				Group: "apps",
			},
		},
		{
			name:  "with complex group",
			input: "CustomResource.custom.io",
			expected: schema.GroupKind{
				Kind:  "CustomResource",
				Group: "custom.io",
			},
		},
		{
			name:  "with dots in group",
			input: "CRD.apiextensions.k8s.io",
			expected: schema.GroupKind{
				Kind:  "CRD",
				Group: "apiextensions.k8s.io",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseGroupKind(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func Test_UnstructuredList(t *testing.T) {
	input := &unstructured.UnstructuredList{
		Items: []unstructured.Unstructured{
			{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"name": "obj1",
					},
				},
			},
			{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"name": "obj2",
					},
				},
			},
			{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"name": "obj3",
					},
				},
			},
		},
	}

	result := unstructuredList(input)

	assert.Len(t, result, 3)
	assert.Equal(t, "obj1", result[0].Object["metadata"].(map[string]interface{})["name"])
	assert.Equal(t, "obj2", result[1].Object["metadata"].(map[string]interface{})["name"])
	assert.Equal(t, "obj3", result[2].Object["metadata"].(map[string]interface{})["name"])
}

func Test_New_ValidationErrors(t *testing.T) {
	parent := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-parent",
			Namespace: "default",
		},
	}
	parent.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "ConfigMap",
	})

	ctx := context.Background()

	t.Run("missing toolingID", func(t *testing.T) {
		_, err := New(ctx, parent, nil, nil, Config{
			ToolingID:    ToolingID{}, // empty
			FieldManager: "test-manager",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "toolingID is required")
	})

	t.Run("missing fieldManager", func(t *testing.T) {
		_, err := New(ctx, parent, nil, nil, Config{
			ToolingID:    ToolingID{Name: "test", Version: "v1"},
			FieldManager: "", // empty
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "fieldManager is required")
	})
}

func Test_ApplySet_ID(t *testing.T) {
	parent := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-parent",
			Namespace: "default",
		},
	}
	parent.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "ConfigMap",
	})

	aset := &applySet{
		parent: parent,
	}

	id := aset.ID()
	assert.NotEmpty(t, id)
	assert.Contains(t, id, "applyset-")

	// ID should be consistent
	id2 := aset.ID()
	assert.Equal(t, id, id2)
}

func Test_Constants(t *testing.T) {
	// Verify that constants are set as expected
	assert.Equal(t, ".", ApplySetIDPartDelimiter)
	assert.Equal(t, "applyset-%s", V1ApplySetIdFormat)
	assert.Equal(t, "applyset.k8s.io/id", ApplySetParentIDLabel)
	assert.Equal(t, "applyset.k8s.io/part-of", ApplySetPartOfLabel)
	assert.Equal(t, "applyset.k8s.io/tooling", ApplySetToolingAnnotation)
	assert.Equal(t, "applyset.k8s.io/contains-group-kinds", ApplySetGKsAnnotation)
	assert.Equal(t, "applyset.k8s.io/additional-namespaces", ApplySetAdditionalNamespacesAnnotation)
}
