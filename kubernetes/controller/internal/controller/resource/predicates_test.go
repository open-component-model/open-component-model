package resource

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/fluxcd/pkg/apis/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
)

var _ = Describe("ComponentInfoChangedPredicate", func() {
	var predicate ComponentInfoChangedPredicate

	BeforeEach(func() {
		predicate = ComponentInfoChangedPredicate{}
	})

	Describe("Create / Delete / Generic", func() {
		It("always allows Create events", func() {
			Expect(predicate.Create(event.CreateEvent{})).To(BeTrue())
		})
		It("always allows Delete events", func() {
			Expect(predicate.Delete(event.DeleteEvent{})).To(BeTrue())
		})
		It("always allows Generic events", func() {
			Expect(predicate.Generic(event.GenericEvent{})).To(BeTrue())
		})
	})

	Describe("Update", func() {
		var oldComponent, newComponent *v1alpha1.Component

		BeforeEach(func() {
			oldComponent = &v1alpha1.Component{}
			newComponent = &v1alpha1.Component{}
		})

		It("allows the event when ObjectOld is nil", func() {
			Expect(predicate.Update(event.UpdateEvent{
				ObjectOld: nil,
				ObjectNew: newComponent,
			})).To(BeTrue())
		})

		It("allows the event when ObjectNew is nil", func() {
			Expect(predicate.Update(event.UpdateEvent{
				ObjectOld: oldComponent,
				ObjectNew: nil,
			})).To(BeTrue())
		})

		It("rejects the event when objects are not Components", func() {
			Expect(predicate.Update(event.UpdateEvent{
				ObjectOld: &v1alpha1.Resource{},
				ObjectNew: &v1alpha1.Resource{},
			})).To(BeFalse())
		})

		It("filters the event when nothing relevant changed", func() {
			Expect(predicate.Update(event.UpdateEvent{
				ObjectOld: oldComponent,
				ObjectNew: newComponent,
			})).To(BeFalse())
		})

		It("allows the event when Status.Component changed", func() {
			newComponent.Status.Component.Component = "changed-component"
			Expect(predicate.Update(event.UpdateEvent{
				ObjectOld: oldComponent,
				ObjectNew: newComponent,
			})).To(BeTrue())
		})

		It("allows the event when Status.EffectiveOCMConfig changed", func() {
			newComponent.Status.EffectiveOCMConfig = []v1alpha1.OCMConfiguration{
				{
					NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
						Name:      "my-config",
						Namespace: "default",
						Kind:      "ConfigMap",
					},
					Policy: v1alpha1.ConfigurationPolicyPropagate,
				},
			}
			Expect(predicate.Update(event.UpdateEvent{
				ObjectOld: oldComponent,
				ObjectNew: newComponent,
			})).To(BeTrue())
		})

		It("filters the event when EffectiveOCMConfig is equal on both sides", func() {
			cfg := []v1alpha1.OCMConfiguration{
				{
					NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
						Name:      "my-config",
						Namespace: "default",
						Kind:      "ConfigMap",
					},
					Policy: v1alpha1.ConfigurationPolicyPropagate,
				},
			}
			oldComponent.Status.EffectiveOCMConfig = cfg
			newComponent.Status.EffectiveOCMConfig = cfg
			Expect(predicate.Update(event.UpdateEvent{
				ObjectOld: oldComponent,
				ObjectNew: newComponent,
			})).To(BeFalse())
		})

		It("allows the event when readiness transitions from not-ready to ready", func() {
			newComponent.Status.Conditions = []metav1.Condition{
				{
					Type:               meta.ReadyCondition,
					Status:             metav1.ConditionTrue,
					LastTransitionTime: metav1.Now(),
					Reason:             "Ready",
				},
			}
			Expect(predicate.Update(event.UpdateEvent{
				ObjectOld: oldComponent,
				ObjectNew: newComponent,
			})).To(BeTrue())
		})

		It("filters the event when only a non-readiness condition changed", func() {
			readyCond := metav1.Condition{
				Type:               meta.ReadyCondition,
				Status:             metav1.ConditionTrue,
				LastTransitionTime: metav1.Now(),
				Reason:             "Ready",
			}
			oldComponent.Status.Conditions = []metav1.Condition{readyCond}
			newComponent.Status.Conditions = []metav1.Condition{
				readyCond,
				{
					Type:               "SomeOtherCondition",
					Status:             metav1.ConditionTrue,
					LastTransitionTime: metav1.Now(),
					Reason:             "Reason",
				},
			}
			Expect(predicate.Update(event.UpdateEvent{
				ObjectOld: oldComponent,
				ObjectNew: newComponent,
			})).To(BeFalse())
		})
	})
})
