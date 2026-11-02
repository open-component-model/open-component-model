package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/controller/resource"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/ocm"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/resolution"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/setup"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/status"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/test"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
)

const waitTimeout = 2 * time.Minute

// lockedBuffer collects the controller log, which the manager writes from its own goroutines.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// startResourceController starts an API server and a manager running the Resource reconciler
// with the plugin managers the controller builds in production. The controller log is
// printed if the test fails.
func startResourceController(t *testing.T) client.Client {
	t.Helper()
	r := require.New(t)

	logs := &lockedBuffer{}
	logger := zap.New(zap.WriteTo(logs), zap.UseDevMode(true))
	logf.SetLogger(logger)
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("controller log:\n%s", logs.String())
		}
	})

	testEnv := test.NewEnvTest()
	cfg, err := testEnv.Start()
	r.NoError(err)

	scheme := k8sruntime.NewScheme()
	r.NoError(clientgoscheme.AddToScheme(scheme))
	r.NoError(v1alpha1.AddToScheme(scheme))

	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme})
	r.NoError(err)

	gracefulTimeout := 5 * time.Second
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:                  scheme,
		GracefulShutdownTimeout: &gracefulTimeout,
		Metrics:                 metricserver.Options{BindAddress: "0"},
	})
	r.NoError(err)

	ctx, cancel := context.WithCancel(t.Context())

	// As in cmd/main.go: a plugin manager per request, built from the request's configuration,
	// sharing one blob cache.
	pluginLogger := slog.New(logr.ToSlogHandler(logger))
	tempDir := t.TempDir()
	blobCache, err := setup.NewBlobCache(tempDir)
	r.NoError(err)
	newPluginManager := func(ctx context.Context, cfg *genericv1.Config) (*manager.PluginManager, error) {
		return setup.NewPluginManager(ctx, cfg, pluginLogger, setup.WithTempDir(tempDir), setup.WithBlobCache(blobCache))
	}

	resolutionLogger := logger.WithName("resolution")
	r.NoError((&resource.Reconciler{
		BaseReconciler: &ocm.BaseReconciler{
			Client:           mgr.GetClient(),
			Scheme:           scheme,
			EventRecorder:    &record.FakeRecorder{},
			NewPluginManager: newPluginManager,
		},
		Resolver: resolution.NewResolver(&resolutionLogger),
	}).SetupWithManager(ctx, mgr, 4))

	mgrDone := make(chan error, 1)
	go func() { mgrDone <- mgr.Start(ctx) }()

	t.Cleanup(func() {
		cancel()
		if err := <-mgrDone; err != nil {
			assert.ErrorContains(t, err, "grace period")
		}
		assert.NoError(t, testEnv.Stop())
	})

	return k8sClient
}

// createOCMConfigSecret stores an OCM configuration in a Secret for resources to reference.
func createOCMConfigSecret(t *testing.T, ctx context.Context, c client.Client, namespace, name string, config []byte) v1alpha1.OCMConfiguration {
	t.Helper()
	require.NoError(t, c.Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Data:       map[string][]byte{v1alpha1.OCMConfigKey: config},
	}))
	return v1alpha1.OCMConfiguration{
		NamespacedObjectKindReference: v1alpha1.NamespacedObjectKindReference{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "Secret",
			Name:       name,
			Namespace:  namespace,
		},
		Policy: v1alpha1.ConfigurationPolicyDoNotPropagate,
	}
}

// createReadyComponent creates a Component and marks it Ready for componentName:version in
// the repository repositorySpec describes. No Component controller runs, so it stays so.
func createReadyComponent(t *testing.T, ctx context.Context, c client.Client, namespace, name, componentName, version string, repositorySpec []byte) *v1alpha1.Component {
	t.Helper()
	r := require.New(t)

	component := &v1alpha1.Component{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: v1alpha1.ComponentSpec{
			RepositoryRef: corev1.LocalObjectReference{Name: "repository"},
			Component:     componentName,
		},
	}
	r.NoError(c.Create(ctx, component))

	old := component.DeepCopy()
	component.Status.Component = v1alpha1.ComponentInfo{
		Component:      componentName,
		Version:        version,
		RepositorySpec: &apiextensionsv1.JSON{Raw: repositorySpec},
	}
	status.MarkReady(&record.FakeRecorder{}, component, "applied component")
	component.SetObservedGeneration(component.GetGeneration())
	r.NoError(c.Status().Patch(ctx, component, client.MergeFrom(old)))

	return component
}

// readyCondition returns the Ready condition of the Resource, after reading it.
func readyCondition(ctx context.Context, c client.Client, obj *v1alpha1.Resource) (*metav1.Condition, error) {
	if err := c.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
		return nil, err
	}
	cond := apimeta.FindStatusCondition(obj.GetConditions(), v1alpha1.ReadyCondition)
	if cond == nil {
		return nil, fmt.Errorf("resource %s has no Ready condition", obj.GetName())
	}
	return cond, nil
}

// waitForReady waits until the Resource is Ready for its current generation.
func waitForReady(t *testing.T, ctx context.Context, c client.Client, obj *v1alpha1.Resource) {
	t.Helper()
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		cond, err := readyCondition(ctx, c, obj)
		if !assert.NoError(ct, err) {
			return
		}
		assert.Equal(ct, obj.GetGeneration(), obj.Status.ObservedGeneration)
		assert.Equal(ct, metav1.ConditionTrue, cond.Status, "Ready condition: %s: %s", cond.Reason, cond.Message)
	}, waitTimeout, 250*time.Millisecond)
}

// waitForNotReady waits until the Resource is not Ready for reason with a message containing
// all of messages.
func waitForNotReady(t *testing.T, ctx context.Context, c client.Client, obj *v1alpha1.Resource, reason string, messages ...string) {
	t.Helper()
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		cond, err := readyCondition(ctx, c, obj)
		if !assert.NoError(ct, err) {
			return
		}
		assert.Equal(ct, metav1.ConditionFalse, cond.Status, "Ready condition: %s: %s", cond.Reason, cond.Message)
		assert.Equal(ct, reason, cond.Reason, "Ready condition: %s: %s", cond.Reason, cond.Message)
		for _, message := range messages {
			assert.Contains(ct, cond.Message, message)
		}
	}, waitTimeout, 250*time.Millisecond)
	t.Logf("not ready as expected: %s", apimeta.FindStatusCondition(obj.GetConditions(), v1alpha1.ReadyCondition).Message)
}

// mustJSON marshals v, which the test builds and is always valid.
func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	return data
}
