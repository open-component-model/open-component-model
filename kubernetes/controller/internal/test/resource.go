package test

import (
	"context"
	"errors"

	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/kubernetes/controller/internal/status"
)

type MockResourceOptions struct {
	ComponentRef       corev1.LocalObjectReference
	ComponentInfo      *v1alpha1.ComponentInfo
	ResourceInfo       *v1alpha1.ResourceInfo
	EffectiveOCMConfig []v1alpha1.OCMConfiguration

	Clnt     client.Client
	Recorder record.EventRecorder
}

func MockResource(
	ctx context.Context,
	name, namespace string,
	options *MockResourceOptions,
) *v1alpha1.Resource {
	resource := &v1alpha1.Resource{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: v1alpha1.ResourceSpec{
			Resource: v1alpha1.ResourceID{
				ByReference: v1alpha1.ResourceReference{
					Resource: runtime.Identity{"name": name},
				},
			},
			ComponentRef: options.ComponentRef,
		},
	}
	Expect(options.Clnt.Create(ctx, resource)).To(Succeed())

	old := resource.DeepCopy()

	resource.Status.Component = options.ComponentInfo
	resource.Status.Resource = options.ResourceInfo
	resource.Status.EffectiveOCMConfig = options.EffectiveOCMConfig

	status.MarkReady(options.Recorder, resource, "applied mock resource")
	resource.SetObservedGeneration(resource.GetGeneration())
	Expect(options.Clnt.Status().Patch(ctx, resource, client.MergeFrom(old))).To(Succeed())

	Eventually(func(ctx context.Context) error {
		r := &v1alpha1.Resource{}
		Expect(options.Clnt.Get(ctx, client.ObjectKeyFromObject(resource), r)).To(Succeed())

		if apimeta.IsStatusConditionTrue(r.GetConditions(), v1alpha1.ReadyCondition) {
			return nil
		}

		return errors.New("resource is not ready")
	}).WithContext(ctx).Should(Succeed())

	return resource
}
