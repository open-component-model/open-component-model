package applyset_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"ocm.software/open-component-model/kubernetes/controller/internal/controller/applyset"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func Test_ComputeID(t *testing.T) {
	cases := []struct {
		name       string
		parent     client.Object
		expectedID string
	}{
		{
			name: "namespaced ConfigMap in default namespace",
			parent: func() client.Object {
				cm := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-config",
						Namespace: "default",
					},
				}
				cm.SetGroupVersionKind(schema.GroupVersionKind{
					Group:   "",
					Version: "v1",
					Kind:    "ConfigMap",
				})
				return cm
			}(),
			expectedID: "applyset-WMpXSab3F471pG8yj3lUlnLzJlsAUO3Y1u0xyU39H8o",
		},
		{
			name: "namespaced ConfigMap in kube-system namespace",
			parent: func() client.Object {
				cm := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-config",
						Namespace: "kube-system",
					},
				}
				cm.SetGroupVersionKind(schema.GroupVersionKind{
					Group:   "",
					Version: "v1",
					Kind:    "ConfigMap",
				})
				return cm
			}(),
			expectedID: "applyset-jmaR-_71xlQkLbPr1uahJhyO41_HAk_6TL5nfwnaA7w",
		},
		{
			name: "cluster-scoped Namespace",
			parent: func() client.Object {
				ns := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "my-namespace",
					},
				}
				ns.SetGroupVersionKind(schema.GroupVersionKind{
					Group:   "",
					Version: "v1",
					Kind:    "Namespace",
				})
				return ns
			}(),
			expectedID: "applyset-6h7Khpy4q5bwx-r6uEd1CVAqfsDwoua515LWGYfBvZs",
		},
		{
			name: "namespaced Secret in default namespace",
			parent: func() client.Object {
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-secret",
						Namespace: "default",
					},
				}
				secret.SetGroupVersionKind(schema.GroupVersionKind{
					Group:   "",
					Version: "v1",
					Kind:    "Secret",
				})
				return secret
			}(),
			expectedID: "applyset-oSiAHx7f15nv1BdrRJdV6Bqk-qPZ7z0LkS_OxKu43rQ",
		},
		{
			name: "custom resource with group",
			parent: func() client.Object {
				obj := &unstructured.Unstructured{}
				obj.SetName("my-deployer")
				obj.SetNamespace("default")
				obj.SetGroupVersionKind(schema.GroupVersionKind{
					Group:   "delivery.ocm.software",
					Version: "v1alpha1",
					Kind:    "Deployer",
				})
				return obj
			}(),
			expectedID: "applyset-D77EJtZgpU1950wAGLHuiE172RQcNwtcLfGHcoG0Ydg",
		},
		{
			name: "custom resource in different namespace",
			parent: func() client.Object {
				obj := &unstructured.Unstructured{}
				obj.SetName("my-deployer")
				obj.SetNamespace("production")
				obj.SetGroupVersionKind(schema.GroupVersionKind{
					Group:   "delivery.ocm.software",
					Version: "v1alpha1",
					Kind:    "Deployer",
				})
				return obj
			}(),
			expectedID: "applyset-i47kAg3-o6Rs1IVLnhxSkr_wpJhPWMZNBPdeT-L7_Pk",
		},
		{
			name: "same name different namespace produces different ID",
			parent: func() client.Object {
				cm := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-config",
						Namespace: "namespace-a",
					},
				}
				cm.SetGroupVersionKind(schema.GroupVersionKind{
					Group:   "",
					Version: "v1",
					Kind:    "ConfigMap",
				})
				return cm
			}(),
			expectedID: "applyset-hA8A-ACIxRpv-tLSrlWg4RveZ90SucKzxGln3KSSnn4",
		},
		{
			name: "same namespace different name produces different ID",
			parent: func() client.Object {
				cm := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "different-config",
						Namespace: "default",
					},
				}
				cm.SetGroupVersionKind(schema.GroupVersionKind{
					Group:   "",
					Version: "v1",
					Kind:    "ConfigMap",
				})
				return cm
			}(),
			expectedID: "applyset-qOnMjK6UMfG9m5jEyXQhY-q8YaNg4TS40pMMynRV3zw",
		},
		{
			name: "same name and namespace but different kind produces different ID",
			parent: func() client.Object {
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-config",
						Namespace: "default",
					},
				}
				secret.SetGroupVersionKind(schema.GroupVersionKind{
					Group:   "",
					Version: "v1",
					Kind:    "Secret",
				})
				return secret
			}(),
			expectedID: "applyset-IsPfNLnDhmAttrRpUQeluhpLLVFO8qgGevAOm2BMiBg",
		},
		{
			name: "name with special characters",
			parent: func() client.Object {
				cm := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-config-123.v2",
						Namespace: "default",
					},
				}
				cm.SetGroupVersionKind(schema.GroupVersionKind{
					Group:   "",
					Version: "v1",
					Kind:    "ConfigMap",
				})
				return cm
			}(),
			expectedID: "applyset-35AvIRO2A_CZkp2pOtGKwHyBGLoQ2QEh_6cZSCh2l3E",
		},
		{
			name: "namespace with special characters",
			parent: func() client.Object {
				cm := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-config",
						Namespace: "team-prod-123",
					},
				}
				cm.SetGroupVersionKind(schema.GroupVersionKind{
					Group:   "",
					Version: "v1",
					Kind:    "ConfigMap",
				})
				return cm
			}(),
			expectedID: "applyset-X0V9kZ3qdnKY5VMz0swyxmGHZ7VBrdLFsJvqYSwvVtE",
		},
		{
			name: "cluster-scoped custom resource",
			parent: func() client.Object {
				obj := &unstructured.Unstructured{}
				obj.SetName("cluster-resource")
				obj.SetGroupVersionKind(schema.GroupVersionKind{
					Group:   "custom.io",
					Version: "v1",
					Kind:    "ClusterResource",
				})
				return obj
			}(),
			expectedID: "applyset-kcCIqkAMghVlPk4HvUDMoRKEjf3YhUZAebnRnarqfvY",
		},
		{
			name: "object with long name",
			parent: func() client.Object {
				cm := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "this-is-a-very-long-name-for-testing-purposes-with-many-characters",
						Namespace: "default",
					},
				}
				cm.SetGroupVersionKind(schema.GroupVersionKind{
					Group:   "",
					Version: "v1",
					Kind:    "ConfigMap",
				})
				return cm
			}(),
			expectedID: "applyset-2GIdv55R8n2hnKaS6vqTu62XzIAhxLK0_jA-vWTTo3o",
		},
		{
			name: "apiextensions.k8s.io group",
			parent: func() client.Object {
				obj := &unstructured.Unstructured{}
				obj.SetName("my-crd")
				obj.SetGroupVersionKind(schema.GroupVersionKind{
					Group:   "apiextensions.k8s.io",
					Version: "v1",
					Kind:    "CustomResourceDefinition",
				})
				return obj
			}(),
			expectedID: "applyset-kcFVkXqLW-12N-Hv_C5VVBBp2MGNj21dHFsuaQ3Ih-M",
		},
		{
			name: "rbac.authorization.k8s.io group",
			parent: func() client.Object {
				obj := &unstructured.Unstructured{}
				obj.SetName("my-role")
				obj.SetNamespace("default")
				obj.SetGroupVersionKind(schema.GroupVersionKind{
					Group:   "rbac.authorization.k8s.io",
					Version: "v1",
					Kind:    "Role",
				})
				return obj
			}(),
			expectedID: "applyset-1qQlFek9WdzcYX9dXUgCX0MzYBq6EC4RtggasIfVtaI",
		},
		{
			name: "should never exceed length limits",
			parent: func() client.Object {
				cm := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "very-long-configmap-name-to-test-the-maximum-length-of-the-computed-id-in-the-applyset-controller",
						Namespace: "very-long-namespace-name-to-test-the-maximum-length-of-the-computed-id-in-the-applyset-controller",
					},
				}
				cm.SetGroupVersionKind(schema.GroupVersionKind{
					Group:   "ocm.software.longgroupname",
					Version: "v1alpha1beta2",
					Kind:    "ConfigMap",
				})
				return cm
			}(),
			expectedID: "applyset-1UQZMIgIAU1QpDpAO_EsOHlbgmhG1NX8q4FhlZ-KGfI",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := applyset.ComputeID(tc.parent)
			assert.Equal(t, tc.expectedID, id)

			// Verify the ID format
			assert.Contains(t, id, "applyset-", "ID should start with 'applyset-' prefix")
			assert.Greater(t, len(id), len("applyset-"), "ID should have content after prefix")
		})
	}
}

// Test_ComputeID_Consistency verifies that the same input produces the same ID
func Test_ComputeID_Consistency(t *testing.T) {
	createParent := func() client.Object {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-config",
				Namespace: "test-ns",
			},
		}
		cm.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "",
			Version: "v1",
			Kind:    "ConfigMap",
		})
		return cm
	}

	parent1 := createParent()
	parent2 := createParent()

	id1 := applyset.ComputeID(parent1)
	id2 := applyset.ComputeID(parent2)

	assert.Equal(t, id1, id2, "Same parent configuration should produce the same ID")
}

// Test_ComputeID_Uniqueness verifies that different inputs produce different IDs
func Test_ComputeID_Uniqueness(t *testing.T) {
	parents := []client.Object{
		func() client.Object {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "config-1",
					Namespace: "default",
				},
			}
			cm.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})
			return cm
		}(),
		func() client.Object {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "config-2",
					Namespace: "default",
				},
			}
			cm.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})
			return cm
		}(),
		func() client.Object {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "config-1",
					Namespace: "other-ns",
				},
			}
			cm.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})
			return cm
		}(),
		func() client.Object {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "config-1",
					Namespace: "default",
				},
			}
			secret.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Secret"})
			return secret
		}(),
	}

	ids := make(map[string]bool)
	for i, parent := range parents {
		id := applyset.ComputeID(parent)
		require.NotEmpty(t, id, "ID should not be empty for parent %d", i)

		if ids[id] {
			t.Errorf("Duplicate ID found: %s for parent %d", id, i)
		}
		ids[id] = true
	}

	assert.Equal(t, len(parents), len(ids), "All IDs should be unique")
}

// Test_ComputeID_Format verifies the ID format follows the specification
func Test_ComputeID_Format(t *testing.T) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
	}
	cm.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "ConfigMap",
	})

	id := applyset.ComputeID(cm)

	// Verify format: applyset-<base64-encoded-sha256>
	assert.True(t, len(id) > len("applyset-"), "ID should have content after prefix")
	assert.Contains(t, id, "applyset-", "ID should start with 'applyset-' prefix")

	// Base64 URL-safe encoding uses these characters: A-Z, a-z, 0-9, -, _
	// The ID after the prefix should only contain these characters
	idContent := id[len("applyset-"):]
	for _, char := range idContent {
		valid := (char >= 'A' && char <= 'Z') ||
			(char >= 'a' && char <= 'z') ||
			(char >= '0' && char <= '9') ||
			char == '-' || char == '_'
		assert.True(t, valid, "ID should only contain base64 URL-safe characters, found: %c", char)
	}
}

// Test_ComputeID_EmptyNamespace verifies cluster-scoped resources (empty namespace)
func Test_ComputeID_EmptyNamespace(t *testing.T) {
	// Cluster-scoped resource with explicitly empty namespace
	ns1 := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-namespace",
			Namespace: "", // Explicitly empty for cluster-scoped
		},
	}
	ns1.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "Namespace",
	})

	// Another cluster-scoped resource
	ns2 := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "my-namespace",
			// Namespace field not set (implicitly empty)
		},
	}
	ns2.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "Namespace",
	})

	id1 := applyset.ComputeID(ns1)
	id2 := applyset.ComputeID(ns2)

	assert.Equal(t, id1, id2, "Explicit and implicit empty namespace should produce same ID")
	assert.NotEmpty(t, id1, "ID should not be empty for cluster-scoped resource")
}
