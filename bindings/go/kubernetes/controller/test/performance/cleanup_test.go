package main

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
)

func TestCleanup(t *testing.T) {
	for _, tt := range []struct {
		name       string
		finalizers []string
		wantErr    bool
	}{
		{name: "deletes the pipelines and the namespace"},
		{name: "a stuck Deployer does not keep the namespace", finalizers: []string{"example.com/stuck"}, wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			scheme := runtime.NewScheme()
			r.NoError(clientgoscheme.AddToScheme(scheme))
			r.NoError(v1alpha1.AddToScheme(scheme))

			w := newWorkload(scenarioComplex, 1, "run", "http://registry:5000")
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: w.namespace, Labels: map[string]string{runLabel: w.runID}}}
			d := w.deployer(0)
			d.Finalizers = tt.finalizers
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(ns, d, w.component(0)).Build()

			run := &runner{c: &cluster{client: c, scheme: scheme}, opts: options{timeout: 300 * time.Millisecond}}
			err := run.cleanup(t.Context(), slog.Default(), w)
			if tt.wantErr {
				r.Error(err)
			} else {
				r.NoError(err)
			}

			err = c.Get(t.Context(), client.ObjectKeyFromObject(ns), &corev1.Namespace{})
			r.True(apierrors.IsNotFound(err), "namespace %s was not deleted: %v", w.namespace, err)
			err = c.Get(t.Context(), client.ObjectKeyFromObject(w.component(0)), &v1alpha1.Component{})
			r.True(apierrors.IsNotFound(err), "component was not deleted: %v", err)
		})
	}
}

func TestWaitGoneRetriesFailedLists(t *testing.T) {
	r := require.New(t)
	scheme := runtime.NewScheme()
	r.NoError(clientgoscheme.AddToScheme(scheme))

	failed := false
	c := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if !failed {
				failed = true
				return apierrors.NewServiceUnavailable("etcd leader changed")
			}
			return c.List(ctx, list, opts...)
		},
	}).Build()

	cl := &cluster{client: c, scheme: scheme}
	r.NoError(cl.waitGone(t.Context(), &corev1.Namespace{}, "run", 5*time.Second))
	r.True(failed)
}

func TestCleanupDeletesEveryNamespaceDespiteAFailure(t *testing.T) {
	r := require.New(t)
	scheme := runtime.NewScheme()
	r.NoError(clientgoscheme.AddToScheme(scheme))
	r.NoError(v1alpha1.AddToScheme(scheme))

	w := newWorkload(scenarioShared, 2, "run", "http://registry:5000")
	failing, other := w.namespaceOf(0), w.namespaceOf(1)
	var objs []client.Object
	for _, name := range w.namespaces() {
		objs = append(objs, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{runLabel: w.runID}}})
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).WithInterceptorFuncs(interceptor.Funcs{
		Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			if _, ok := obj.(*corev1.Namespace); ok {
				switch obj.GetName() {
				case failing:
					return apierrors.NewServiceUnavailable("etcd leader changed")
				case other:
					select {
					case <-ctx.Done():
						return ctx.Err()
					case <-time.After(200 * time.Millisecond):
					}
				}
			}
			return c.Delete(ctx, obj, opts...)
		},
	}).Build()

	run := &runner{c: &cluster{client: c, scheme: scheme}, opts: options{timeout: 300 * time.Millisecond}}
	r.Error(run.cleanup(t.Context(), slog.Default(), w))

	err := c.Get(t.Context(), client.ObjectKey{Name: other}, &corev1.Namespace{})
	r.True(apierrors.IsNotFound(err), "namespace %s was not deleted: %v", other, err)
}
