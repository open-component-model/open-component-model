package main

import (
	"context"
	"fmt"

	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
)

const changeWorkers = 32

// phase is one measured step of a run: make a change, then watch one kind of object
// until the object of every pipeline shows that the change landed.
type phase struct {
	name string
	// prepare runs before the window opens, so its time is reported but not measured. Optional.
	prepare func(context.Context) error
	// change is the measured change.
	change func(context.Context) error
	// watch is the kind that shows the change, one object per pipeline.
	watch schema.GroupVersionKind
	// until reports whether one object shows the change. Objects that exist before the
	// change are not asked: only what the tracker sees after the change counts.
	until func(*unstructured.Unstructured) objectState
}

// phases are the measured phases of a run, in order. Each one starts from the state the
// one before left: every pipeline done and the controller queues drained.
func (r *run) phases() []phase {
	return []phase{r.rollout(), r.drift()}
}

// rollout publishes the component versions and creates every pipeline; done when every
// Deployer is Ready. Models the first install, with cold caches.
func (r *run) rollout() phase {
	return phase{
		name: "rollout",
		prepare: func(ctx context.Context) error {
			if err := r.pub.publishImages(ctx, r.w.imagePool()); err != nil {
				return err
			}
			cvs, err := r.w.componentVersions()
			if err != nil {
				return err
			}
			r.log.Info("publishing component versions", "count", len(cvs))
			return r.pub.publish(ctx, cvs)
		},
		change: r.create,
		watch:  deployerGVK,
		until:  readyState,
	}
}

// drift changes the version of every deployed ConfigMap, the way a person editing a
// deployed object would; done when every Deployer reverted its own. Models any update
// of a deployed object, status churn included, which enqueues its Deployer: warm
// caches, the same digest.
func (r *run) drift() phase {
	return phase{
		name:   "drift",
		change: r.driftConfigMaps,
		watch:  configMapGVK,
		until:  configMapVersionState(initialVersion),
	}
}

// create applies the Repositories, the signing Secret and the deployer RBAC first, then
// everything else in parallel, the way a GitOps tool would apply a directory.
func (r *run) create(ctx context.Context) error {
	var first, rest []client.Object
	for _, obj := range r.objs {
		switch obj.(type) {
		case *v1alpha1.Repository, *corev1.Secret, *corev1.ServiceAccount, *rbacv1.Role, *rbacv1.RoleBinding:
			first = append(first, obj)
		default:
			rest = append(rest, obj)
		}
	}
	for _, batch := range [][]client.Object{first, rest} {
		if err := forEach(ctx, batch, func(ctx context.Context, obj client.Object) error {
			if err := r.c.client.Create(ctx, obj); err != nil {
				return fmt.Errorf("creating %s: %w", obj.GetName(), err)
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

func (r *run) driftConfigMaps(ctx context.Context) error {
	patch := client.RawPatch(types.MergePatchType, fmt.Appendf(nil, `{"data":{"version":%q}}`, driftedVersion))
	return forEach(ctx, r.w.configMaps(), func(ctx context.Context, key client.ObjectKey) error {
		cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace}}
		if err := r.c.client.Patch(ctx, cm, patch); err != nil {
			return fmt.Errorf("drifting %s: %w", key, err)
		}
		return nil
	})
}

// forEach calls f for every item, changeWorkers at a time, and stops at the first error.
func forEach[T any](ctx context.Context, items []T, f func(context.Context, T) error) error {
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(changeWorkers)
	for _, item := range items {
		g.Go(func() error { return f(ctx, item) })
	}
	return g.Wait()
}

// readyState is done when the object is Ready for its current generation.
func readyState(u *unstructured.Unstructured) objectState {
	conditions, _, _ := unstructured.NestedSlice(u.Object, "status", "conditions")
	observed, _, _ := unstructured.NestedInt64(u.Object, "status", "observedGeneration")
	for _, c := range conditions {
		cond, ok := c.(map[string]any)
		if !ok || cond["type"] != v1alpha1.ReadyCondition {
			continue
		}
		reason, _ := cond["reason"].(string)
		switch cond["status"] {
		case string(metav1.ConditionTrue):
			return objectState{ready: observed >= u.GetGeneration(), reason: reason}
		case string(metav1.ConditionFalse):
			return objectState{failed: true, reason: reason}
		}
	}
	return objectState{}
}

// configMapVersionState is done when a deployed ConfigMap carries version.
func configMapVersionState(version string) func(*unstructured.Unstructured) objectState {
	return func(u *unstructured.Unstructured) objectState {
		v, _, _ := unstructured.NestedString(u.Object, "data", "version")
		return objectState{ready: v == version}
	}
}
