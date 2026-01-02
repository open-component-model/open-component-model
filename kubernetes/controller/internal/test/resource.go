package test

import (
	"context"
	"time"

	. "github.com/onsi/gomega"

	"github.com/fluxcd/pkg/runtime/patch"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/kubernetes/controller/internal/status"
)

type MockResourceOptions struct {
	ComponentRef  corev1.LocalObjectReference
	ComponentInfo *v1alpha1.ComponentInfo
	ResourceInfo  *v1alpha1.ResourceInfo

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

	patchHelper := patch.NewSerialPatcher(resource, options.Clnt)

	resource.Status.Component = options.ComponentInfo
	resource.Status.Resource = options.ResourceInfo

	Eventually(func(ctx context.Context) error {
		status.MarkReady(options.Recorder, resource, "applied mock resource")

		return status.UpdateStatus(ctx, patchHelper, resource, options.Recorder, time.Hour, nil)
	}).WithContext(ctx).Should(Succeed())

	return resource
}
