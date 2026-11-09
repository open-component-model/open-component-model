package main

import (
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
