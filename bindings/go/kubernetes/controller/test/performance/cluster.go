package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
)

const (
	managerContainer = "manager"
	pollInterval     = time.Second
)

// cluster bundles the clients the benchmark uses against the Kind cluster.
type cluster struct {
	cfg       *rest.Config
	client    client.Client
	clientset kubernetes.Interface
	scheme    *runtime.Scheme
	http      *http.Client

	controllerNamespace string
	controllerSelector  labels.Selector
	metricsPort         int
	registryMetricsURL  string
}

func newCluster(cfg *rest.Config, opts options) (*cluster, error) {
	// Creating 1,000 objects at the default client QPS would take minutes and
	// measure the client rate limiter instead of the controller.
	cfg = rest.CopyConfig(cfg)
	cfg.QPS = 200
	cfg.Burst = 400

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return nil, err
	}
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		return nil, err
	}

	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("creating client: %w", err)
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating clientset: %w", err)
	}
	selector, err := labels.Parse(opts.controllerSelector)
	if err != nil {
		return nil, fmt.Errorf("parsing controller selector: %w", err)
	}

	return &cluster{
		cfg:                 cfg,
		client:              c,
		clientset:           cs,
		scheme:              scheme,
		http:                &http.Client{Timeout: 10 * time.Second},
		controllerNamespace: opts.controllerNamespace,
		controllerSelector:  selector,
		metricsPort:         opts.metricsPort,
		registryMetricsURL:  opts.registryMetricsURL,
	}, nil
}

func (c *cluster) controllerDeployment(ctx context.Context) (*appsv1.Deployment, error) {
	var list appsv1.DeploymentList
	if err := c.client.List(ctx, &list, client.InNamespace(c.controllerNamespace),
		client.MatchingLabelsSelector{Selector: c.controllerSelector}); err != nil {
		return nil, err
	}
	if len(list.Items) != 1 {
		return nil, fmt.Errorf("expected one controller deployment matching %q in %s, found %d",
			c.controllerSelector, c.controllerNamespace, len(list.Items))
	}
	return &list.Items[0], nil
}

// controllerPod returns the single running, non-terminating controller pod.
func (c *cluster) controllerPod(ctx context.Context) (*corev1.Pod, error) {
	var list corev1.PodList
	if err := c.client.List(ctx, &list, client.InNamespace(c.controllerNamespace),
		client.MatchingLabelsSelector{Selector: c.controllerSelector}); err != nil {
		return nil, err
	}
	var running []corev1.Pod
	for _, p := range list.Items {
		if p.DeletionTimestamp == nil && p.Status.Phase == corev1.PodRunning {
			running = append(running, p)
		}
	}
	if len(running) != 1 {
		return nil, fmt.Errorf("expected one running controller pod, found %d", len(running))
	}
	return &running[0], nil
}

// restartController rolls the controller so every run starts with empty
// in-memory caches, and waits until exactly one new pod is ready.
func (c *cluster) restartController(ctx context.Context, timeout time.Duration) error {
	dep, err := c.controllerDeployment(ctx)
	if err != nil {
		return err
	}
	patch := fmt.Sprintf(`{"spec":{"template":{"metadata":{"annotations":{"perf.ocm.software/restartedAt":%q}}}}}`,
		time.Now().Format(time.RFC3339Nano))
	if err := c.client.Patch(ctx, dep, client.RawPatch(types.StrategicMergePatchType, []byte(patch))); err != nil {
		return fmt.Errorf("restarting controller: %w", err)
	}

	return wait.PollUntilContextTimeout(ctx, pollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		var d appsv1.Deployment
		if err := c.client.Get(ctx, client.ObjectKeyFromObject(dep), &d); err != nil {
			return false, err
		}
		want := ptrValue(d.Spec.Replicas, 1)
		if d.Status.ObservedGeneration < d.Generation || d.Status.UpdatedReplicas != want ||
			d.Status.AvailableReplicas != want || d.Status.Replicas != want {
			return false, nil
		}
		_, err := c.controllerPod(ctx)
		return err == nil, nil
	})
}

func (c *cluster) scrapeController(ctx context.Context, pod string) ([]byte, error) {
	return c.clientset.CoreV1().Pods(c.controllerNamespace).
		ProxyGet("http", pod, strconv.Itoa(c.metricsPort), "metrics", nil).DoRaw(ctx)
}

func (c *cluster) scrapeKubelet(ctx context.Context, node string) ([]byte, error) {
	return c.clientset.CoreV1().RESTClient().Get().
		AbsPath("/api/v1/nodes", node, "proxy", "metrics", "resource").DoRaw(ctx)
}

func (c *cluster) scrapeRegistry(ctx context.Context) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.registryMetricsURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry metrics: unexpected status %s", resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func (c *cluster) createNamespace(ctx context.Context, name, runID string) error {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{runLabel: runID}}}
	return c.client.Create(ctx, ns)
}

// deleteNamespace waits until the namespace is gone so finalizer work from
// one run does not overlap the next.
func (c *cluster) deleteNamespace(ctx context.Context, name string, timeout time.Duration) error {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
	if err := c.client.Delete(ctx, ns); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return wait.PollUntilContextTimeout(ctx, 2*pollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		err := c.client.Get(ctx, client.ObjectKeyFromObject(ns), ns)
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		return false, err
	})
}

func (c *cluster) deleteDeployers(ctx context.Context, runID string, timeout time.Duration) error {
	selector := client.MatchingLabels{runLabel: runID}
	if err := c.client.DeleteAllOf(ctx, &v1alpha1.Deployer{}, selector); err != nil {
		return err
	}
	return wait.PollUntilContextTimeout(ctx, 2*pollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		var list v1alpha1.DeployerList
		if err := c.client.List(ctx, &list, selector); err != nil {
			return false, err
		}
		return len(list.Items) == 0, nil
	})
}

func (c *cluster) serverVersion() string {
	v, err := c.clientset.Discovery().ServerVersion()
	if err != nil {
		return ""
	}
	return v.GitVersion
}

func ptrValue[T any](p *T, def T) T {
	if p == nil {
		return def
	}
	return *p
}
