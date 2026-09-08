package discovery

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
)

func objectKey(obj client.Object) types.NamespacedName {
	return types.NamespacedName{Namespace: obj.GetNamespace(), Name: obj.GetName()}
}

var discoveryGVK = schema.GroupVersionKind{
	Group:   v1alpha1.GroupVersion.Group,
	Version: v1alpha1.GroupVersion.Version,
	Kind:    v1alpha1.KindDiscovery,
}

// descriptor is a consumer-shaped v2 component descriptor with arbitrary
// nested JSON (labels, access, references) that must survive the apiserver.
var descriptor = map[string]any{
	"meta": map[string]any{"schemaVersion": "v2"},
	"component": map[string]any{
		"name":     "ocm.software/test",
		"version":  "1.0.0",
		"provider": map[string]any{"name": "ocm.software"},
		"labels": []any{
			map[string]any{
				"name":  "structured-label",
				"value": map[string]any{"nested": map[string]any{"list": []any{1, "two", true}}},
			},
		},
		"resources": []any{
			map[string]any{
				"name":     "image",
				"version":  "1.0.0",
				"type":     "ociImage",
				"relation": "external",
				"access": map[string]any{
					"type":           "ociArtifact",
					"imageReference": "ghcr.io/open-component-model/test:1.0.0",
				},
				"labels": []any{map[string]any{"name": "tier", "value": "platform"}},
			},
		},
		"componentReferences": []any{
			map[string]any{
				"name":          "child",
				"componentName": "ocm.software/child",
				"version":       "2.0.0",
			},
		},
	},
}

func toJSONs(descriptors ...map[string]any) []apiextensionsv1.JSON {
	result := make([]apiextensionsv1.JSON, 0, len(descriptors))
	for _, descriptor := range descriptors {
		raw, err := json.Marshal(descriptor)
		Expect(err).NotTo(HaveOccurred())
		result = append(result, apiextensionsv1.JSON{Raw: raw})
	}
	return result
}

func extractedRecord(values map[string]any) v1alpha1.ExtractedRecord {
	record := make(v1alpha1.ExtractedRecord, len(values))
	for name, value := range values {
		raw, err := json.Marshal(value)
		Expect(err).NotTo(HaveOccurred())
		record[name] = apiextensionsv1.JSON{Raw: raw}
	}
	return record
}

// newUnstructured applies the given spec mutation on a minimal valid Discovery
// expressed as unstructured, bypassing Go client-side serialization
// (omitempty/omitzero) to test server-side admission of explicit values.
func newUnstructured(namespace string, mutate func(spec map[string]any)) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(discoveryGVK)
	obj.SetGenerateName("discovery-")
	obj.SetNamespace(namespace)
	spec := map[string]any{
		"componentRef": map[string]any{"name": "releasechannel"},
	}
	mutate(spec)
	obj.Object["spec"] = spec
	return obj
}

var _ = Describe("Discovery API", func() {
	var namespace *corev1.Namespace

	BeforeEach(func(ctx SpecContext) {
		namespace = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "discovery-"},
		}
		Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
		DeferCleanup(func(ctx SpecContext) {
			Expect(k8sClient.Delete(ctx, namespace)).To(Succeed())
		})
	})

	Context("spec admission", func() {
		It("rejects an empty componentRef name", func(ctx SpecContext) {
			obj := newUnstructured(namespace.Name, func(spec map[string]any) {
				spec["componentRef"] = map[string]any{"name": ""}
			})
			Expect(k8sClient.Create(ctx, obj)).To(MatchError(ContainSubstring("name must not be empty")))
		})

		It("rejects an empty extract", func(ctx SpecContext) {
			obj := newUnstructured(namespace.Name, func(spec map[string]any) {
				spec["extract"] = map[string]any{}
			})
			Expect(k8sClient.Create(ctx, obj)).To(MatchError(ContainSubstring(
				"exactly one of byResources, byComponents, or expression must be specified")))
		})

		It("rejects multiple extract modes", func(ctx SpecContext) {
			obj := newUnstructured(namespace.Name, func(spec map[string]any) {
				spec["extract"] = map[string]any{
					"byResources": map[string]any{"name": "resource.name"},
					"expression":  "components",
				}
			})
			Expect(k8sClient.Create(ctx, obj)).To(MatchError(ContainSubstring(
				"exactly one of byResources, byComponents, or expression must be specified")))
		})

		It("accepts an explicitly empty map extraction mode", func(ctx SpecContext) {
			obj := newUnstructured(namespace.Name, func(spec map[string]any) {
				spec["extract"] = map[string]any{"byResources": map[string]any{}}
			})
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			Expect(k8sClient.Get(ctx, objectKey(obj), obj)).To(Succeed())
			extract, found, err := unstructured.NestedMap(obj.Object, "spec", "extract")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(extract).To(HaveKey("byResources"))

			typed := &v1alpha1.Discovery{}
			Expect(k8sClient.Get(ctx, objectKey(obj), typed)).To(Succeed())
			Expect(typed.Spec.Extract).NotTo(BeNil())
			Expect(typed.Spec.Extract.ByResources).NotTo(BeNil())
			Expect(typed.Spec.Extract.ByResources).To(BeEmpty())
		})

		It("rejects an explicitly empty extraction expression", func(ctx SpecContext) {
			obj := newUnstructured(namespace.Name, func(spec map[string]any) {
				spec["extract"] = map[string]any{"expression": ""}
			})
			err := k8sClient.Create(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("expression"))
		})

		It("accepts an empty selector as a no-op selector", func(ctx SpecContext) {
			obj := newUnstructured(namespace.Name, func(spec map[string]any) {
				spec["componentSelector"] = map[string]any{}
			})
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
		})

		It("accepts a Discovery kind in ocmConfig", func(ctx SpecContext) {
			discovery := &v1alpha1.Discovery{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "discovery-",
					Namespace:    namespace.Name,
				},
				Spec: v1alpha1.DiscoverySpec{
					ComponentRef: corev1.LocalObjectReference{Name: "releasechannel"},
					OCMConfig: []v1alpha1.OCMConfiguration{
						{
							NamespacedObjectKindReference: v1alpha1.NamespacedObjectKindReference{
								APIVersion: v1alpha1.GroupVersion.String(),
								Kind:       v1alpha1.KindDiscovery,
								Name:       "some-config-provider",
							},
							Policy: v1alpha1.ConfigurationPolicyPropagate,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, discovery)).To(Succeed())
		})
	})

	Context("status admission", func() {
		var discovery *v1alpha1.Discovery

		BeforeEach(func(ctx SpecContext) {
			discovery = &v1alpha1.Discovery{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "discovery-",
					Namespace:    namespace.Name,
				},
				Spec: v1alpha1.DiscoverySpec{
					ComponentRef: corev1.LocalObjectReference{Name: "releasechannel"},
				},
			}
			Expect(k8sClient.Create(ctx, discovery)).To(Succeed())
		})

		It("distinguishes absent from empty payload fields", func(ctx SpecContext) {
			fetched := &v1alpha1.Discovery{}
			Expect(k8sClient.Get(ctx, objectKey(discovery), fetched)).To(Succeed())
			Expect(fetched.Status.Components).To(BeNil())
			Expect(fetched.Status.Extracted).To(BeNil())

			fetched.Status.Components = []apiextensionsv1.JSON{}
			Expect(k8sClient.Status().Update(ctx, fetched)).To(Succeed())

			after := &v1alpha1.Discovery{}
			Expect(k8sClient.Get(ctx, objectKey(discovery), after)).To(Succeed())
			Expect(after.Status.Components).NotTo(BeNil())
			Expect(after.Status.Components).To(BeEmpty())
			Expect(after.Status.Extracted).To(BeNil())

			raw := &unstructured.Unstructured{}
			raw.SetGroupVersionKind(discoveryGVK)
			Expect(k8sClient.Get(ctx, objectKey(discovery), raw)).To(Succeed())
			components, found, err := unstructured.NestedSlice(raw.Object, "status", "components")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue(), "components: [] must be present, not omitted")
			Expect(components).To(BeEmpty())
			_, found, err = unstructured.NestedSlice(raw.Object, "status", "extracted")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeFalse())
		})

		It("preserves consumer-shaped arbitrary nested JSON", func(ctx SpecContext) {
			fetched := &v1alpha1.Discovery{}
			Expect(k8sClient.Get(ctx, objectKey(discovery), fetched)).To(Succeed())
			fetched.Status.Components = toJSONs(descriptor)
			Expect(k8sClient.Status().Update(ctx, fetched)).To(Succeed())

			after := &v1alpha1.Discovery{}
			Expect(k8sClient.Get(ctx, objectKey(discovery), after)).To(Succeed())
			Expect(after.Status.Components).To(HaveLen(1))

			raw, err := json.Marshal(descriptor)
			Expect(err).NotTo(HaveOccurred())
			Expect(after.Status.Components[0].Raw).To(MatchJSON(raw))
		})

		It("preserves extracted records with arbitrary nested JSON", func(ctx SpecContext) {
			values := map[string]any{
				"imageRef": "ghcr.io/open-component-model/test:1.0.0",
				"metadata": map[string]any{
					"platforms": []any{"linux/amd64", "linux/arm64"},
					"signed":    true,
				},
				"optional": nil,
			}
			fetched := &v1alpha1.Discovery{}
			Expect(k8sClient.Get(ctx, objectKey(discovery), fetched)).To(Succeed())
			fetched.Status.Extracted = []v1alpha1.ExtractedRecord{
				extractedRecord(values),
				{},
			}
			Expect(k8sClient.Status().Update(ctx, fetched)).To(Succeed())

			after := &v1alpha1.Discovery{}
			Expect(k8sClient.Get(ctx, objectKey(discovery), after)).To(Succeed())
			Expect(after.Status.Extracted).To(HaveLen(2))
			Expect(after.Status.Extracted[1]).To(BeEmpty())
			raw, err := json.Marshal(after.Status.Extracted[0])
			Expect(err).NotTo(HaveOccurred())
			expected, err := json.Marshal(values)
			Expect(err).NotTo(HaveOccurred())
			Expect(raw).To(MatchJSON(expected))
		})

		It("rejects mutually exclusive payload fields", func(ctx SpecContext) {
			fetched := &v1alpha1.Discovery{}
			Expect(k8sClient.Get(ctx, objectKey(discovery), fetched)).To(Succeed())
			fetched.Status.Components = toJSONs(descriptor)
			Expect(k8sClient.Status().Update(ctx, fetched)).To(Succeed())

			fetched.Status.Extracted = []v1alpha1.ExtractedRecord{}
			err := k8sClient.Status().Update(ctx, fetched)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("components and extracted cannot be set at the same time"))
		})

		It("retains old-mode status across spec extraction changes", func(ctx SpecContext) {
			fetched := &v1alpha1.Discovery{}
			Expect(k8sClient.Get(ctx, objectKey(discovery), fetched)).To(Succeed())
			fetched.Status.Components = toJSONs(descriptor)
			Expect(k8sClient.Status().Update(ctx, fetched)).To(Succeed())

			// Switching from raw descriptors to extraction must not invalidate
			// the retained status from the previous output mode.
			Expect(k8sClient.Get(ctx, objectKey(discovery), fetched)).To(Succeed())
			fetched.Spec.Extract = &v1alpha1.Extract{ByResources: map[string]string{"name": "resource.name"}}
			Expect(k8sClient.Update(ctx, fetched)).To(Succeed())

			after := &v1alpha1.Discovery{}
			Expect(k8sClient.Get(ctx, objectKey(discovery), after)).To(Succeed())
			Expect(after.Spec.Extract).NotTo(BeNil())
			Expect(after.Status.Components).To(HaveLen(1))
		})
	})
})
