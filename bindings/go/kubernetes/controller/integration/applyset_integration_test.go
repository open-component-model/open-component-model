package integration_test

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	crcache "sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/controller/applyset"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/test"
)

// applyTimeout is how long a single Apply may take. Conflict detection must never wait for the member cache.
const applyTimeout = 2 * time.Second

// Test_Integration_ApplySetConflictDetection covers conflict detection against a real API server with a running
// member cache, which may lag behind the API server or be unable to watch a kind. Neither may let an ApplySet take
// over another ApplySet's object, keep it from an object that was given up, or block an apply.
func Test_Integration_ApplySetConflictDetection(t *testing.T) {
	env := startAPIServer(t)

	t.Run("never takes over an object another ApplySet created after the cache last saw the kind", func(t *testing.T) {
		r := require.New(t)
		ns, deployer := env.namespace(t, "as-race")
		members := env.startMemberCache(t, env.cfg, applyset.MemberCacheOptions{})
		env.waitForSync(t, members, ns)
		lagging := freeze(t, members, ns)

		other := applyset.ID(newParent(ns, "other"))
		for i := range 10 {
			name := fmt.Sprintf("cm-%d", i)
			env.createMember(t, ns, name, other)

			for _, view := range []applyset.MemberLister{lagging, members} {
				result := env.apply(t, deployer, view, ns, name)
				r.ErrorIs(result.Applied[0].Error, applyset.ErrApplySetConflict, name)
				r.Equal(other, env.owner(t, ns, name), name)
			}
		}
	})

	t.Run("applies an object as soon as its former ApplySet has deleted it", func(t *testing.T) {
		r := require.New(t)
		ns, deployer := env.namespace(t, "as-released")
		members := env.startMemberCache(t, env.cfg, applyset.MemberCacheOptions{})
		other := applyset.ID(newParent(ns, "other"))
		env.createMember(t, ns, "cm", other)

		r.Eventually(func() bool {
			list := configMapList()
			synced, err := members.ListSynced(t.Context(), list, client.InNamespace(ns))
			return err == nil && synced && len(list.Items) == 1
		}, waitTimeout, 100*time.Millisecond, "the cache must hold the other ApplySet's member")
		lagging := freeze(t, members, ns)

		r.NoError(env.admin.Delete(t.Context(), &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "cm", Namespace: ns}}))
		result := env.apply(t, deployer, lagging, ns, "cm")
		r.NoError(result.Applied[0].Error)
		r.Equal(applyset.ID(deployer), env.owner(t, ns, "cm"))
	})

	t.Run("does not wait for a kind the cache may not watch", func(t *testing.T) {
		r := require.New(t)
		ns, deployer := env.namespace(t, "as-unwatchable")
		cfg := rest.CopyConfig(env.cfg)
		cfg.Impersonate = rest.ImpersonationConfig{UserName: "member-cache-without-rbac"}
		syncTimeout := time.Second
		members := env.startMemberCache(t, cfg, applyset.MemberCacheOptions{SyncTimeout: syncTimeout})
		other := applyset.ID(newParent(ns, "other"))
		env.createMember(t, ns, "taken", other)

		// Spans the sync timeout, after which the cache drops the informer and leaves the kind to the live list.
		deadline := time.Now().Add(3 * syncTimeout)
		for i := 0; time.Now().Before(deadline); i++ {
			result := env.apply(t, deployer, members, ns, "taken", fmt.Sprintf("free-%d", i))
			r.ErrorIs(itemError(result, "taken"), applyset.ErrApplySetConflict)
			r.NoError(itemError(result, fmt.Sprintf("free-%d", i)))
			time.Sleep(syncTimeout / 4)
		}
		r.Equal(other, env.owner(t, ns, "taken"))
	})

	t.Run("detects conflicts after the cache removed an idle informer", func(t *testing.T) {
		r := require.New(t)
		ns, deployer := env.namespace(t, "as-idle")
		idleTimeout := 2 * time.Second
		members := env.startMemberCache(t, env.cfg, applyset.MemberCacheOptions{IdleTimeout: idleTimeout})
		env.waitForSync(t, members, ns)
		other := applyset.ID(newParent(ns, "other"))
		env.createMember(t, ns, "taken", other)

		// The janitor runs every half idle timeout, so the informer is gone after at most one and a half.
		time.Sleep(idleTimeout*3/2 + 500*time.Millisecond)
		synced, err := members.ListSynced(t.Context(), configMapList(), client.InNamespace(ns))
		r.NoError(err)
		r.False(synced, "the idle informer must have been removed")

		result := env.apply(t, deployer, members, ns, "taken")
		r.ErrorIs(result.Applied[0].Error, applyset.ErrApplySetConflict)
		env.waitForSync(t, members, ns)
	})
}

type apiServer struct {
	cfg    *rest.Config
	admin  client.Client
	scheme *k8sruntime.Scheme
	mapper meta.RESTMapper
}

func startAPIServer(t *testing.T) *apiServer {
	t.Helper()
	r := require.New(t)

	testEnv := test.NewEnvTest()
	cfg, err := testEnv.Start()
	r.NoError(err)
	t.Cleanup(func() { assert.NoError(t, testEnv.Stop()) })

	scheme := k8sruntime.NewScheme()
	r.NoError(clientgoscheme.AddToScheme(scheme))
	r.NoError(v1alpha1.AddToScheme(scheme))
	httpClient, err := rest.HTTPClientFor(cfg)
	r.NoError(err)
	mapper, err := apiutil.NewDynamicRESTMapper(cfg, httpClient)
	r.NoError(err)
	admin, err := client.New(cfg, client.Options{Scheme: scheme, Mapper: mapper})
	r.NoError(err)

	return &apiServer{cfg: cfg, admin: admin, scheme: scheme, mapper: mapper}
}

// namespace creates a namespace and a NamespacedDeployer whose service account may list, create and patch
// ConfigMaps there and nothing else.
func (s *apiServer) namespace(t *testing.T, name string) (string, *v1alpha1.NamespacedDeployer) {
	t.Helper()
	r := require.New(t)
	ctx := t.Context()

	r.NoError(s.admin.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}))
	const sa = "deployer"
	r.NoError(s.admin.Create(ctx, &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: sa, Namespace: name}}))
	r.NoError(s.admin.Create(ctx, &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Name: sa, Namespace: name},
		Rules:      []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"configmaps"}, Verbs: []string{"list", "create", "patch"}}},
	}))
	r.NoError(s.admin.Create(ctx, &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: sa, Namespace: name},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: sa},
		Subjects:   []rbacv1.Subject{{Kind: rbacv1.ServiceAccountKind, Name: sa, Namespace: name}},
	}))

	deployer := newParent(name, "deployer")
	deployer.Spec.ServiceAccountName = sa

	return name, deployer
}

// startMemberCache runs a member cache reading as cfg until the test ends. It builds its HTTP client from cfg, so
// that cfg.Impersonate applies.
func (s *apiServer) startMemberCache(t *testing.T, cfg *rest.Config, opts applyset.MemberCacheOptions) *applyset.MemberCache {
	t.Helper()
	r := require.New(t)

	opts.Cache = crcache.Options{Scheme: s.scheme, Mapper: s.mapper}
	opts.Log = zap.New(zap.WriteTo(t.Output()))
	members, err := applyset.NewMemberCache(cfg, opts)
	r.NoError(err)

	ctx, cancel := context.WithCancel(context.WithoutCancel(t.Context()))
	done := make(chan error, 1)
	go func() { done <- members.Start(ctx) }()
	t.Cleanup(func() {
		cancel()
		assert.NoError(t, <-done)
	})

	return members
}

func (s *apiServer) waitForSync(t *testing.T, members *applyset.MemberCache, ns string) {
	t.Helper()
	require.Eventually(t, func() bool {
		synced, err := members.ListSynced(t.Context(), configMapList(), client.InNamespace(ns))
		return err == nil && synced
	}, waitTimeout, 100*time.Millisecond, "the member cache must sync ConfigMaps")
}

// apply applies ConfigMaps as the deployer's service account, the way a NamespacedDeployer does, and fails the test
// if conflict detection takes longer than applyTimeout.
func (s *apiServer) apply(t *testing.T, deployer *v1alpha1.NamespacedDeployer, members applyset.MemberLister, ns string, names ...string) *applyset.ApplyResult {
	t.Helper()
	r := require.New(t)

	cfg := rest.CopyConfig(s.cfg)
	cfg.Impersonate = rest.ImpersonationConfig{
		UserName: fmt.Sprintf("system:serviceaccount:%s:%s", ns, deployer.Spec.ServiceAccountName),
	}
	impersonated, err := client.New(cfg, client.Options{Scheme: s.scheme, Mapper: s.mapper})
	r.NoError(err)

	resources := make([]applyset.Resource, 0, len(names))
	for _, name := range names {
		resources = append(resources, applyset.Resource{ID: name, Object: &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata":   map[string]any{"name": name, "namespace": ns},
		}}})
	}

	set := applyset.New(applyset.Config{
		Client:          impersonated,
		Reader:          impersonated,
		Members:         members,
		RESTMapper:      s.mapper,
		Log:             zap.New(zap.WriteTo(t.Output())),
		ParentNamespace: ns,
	}, deployer)

	ctx, cancel := context.WithTimeout(t.Context(), applyTimeout)
	defer cancel()
	start := time.Now()
	result, err := set.Apply(ctx, resources, applyset.ApplyMode{})
	r.NoError(err)
	r.Less(time.Since(start), applyTimeout)
	r.Len(result.Applied, len(names))

	return result
}

// createMember creates a ConfigMap as a member of another ApplySet, as its own tooling would.
func (s *apiServer) createMember(t *testing.T, ns, name, owner string) {
	t.Helper()
	require.NoError(t, s.admin.Create(t.Context(), &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name:      name,
		Namespace: ns,
		Labels:    map[string]string{applyset.ApplysetPartOfLabel: owner},
	}}))
}

func (s *apiServer) owner(t *testing.T, ns, name string) string {
	t.Helper()
	cm := &corev1.ConfigMap{}
	require.NoError(t, s.admin.Get(t.Context(), client.ObjectKey{Namespace: ns, Name: name}, cm))

	return cm.GetLabels()[applyset.ApplysetPartOfLabel]
}

func newParent(ns, name string) *v1alpha1.NamespacedDeployer {
	parent := &v1alpha1.NamespacedDeployer{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}}
	parent.SetGroupVersionKind(v1alpha1.GroupVersion.WithKind(v1alpha1.KindNamespacedDeployer))

	return parent
}

func configMapList() *metav1.PartialObjectMetadataList {
	list := &metav1.PartialObjectMetadataList{}
	list.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ConfigMapList"))

	return list
}

// frozenMembers answers with what a member cache held at one moment, as a cache whose watch lags behind the API
// server does. A real watch delivers too fast to test that lag reliably.
type frozenMembers []metav1.PartialObjectMetadata

func freeze(t *testing.T, members *applyset.MemberCache, ns string) frozenMembers {
	t.Helper()
	list := configMapList()
	synced, err := members.ListSynced(t.Context(), list, client.InNamespace(ns))
	require.NoError(t, err)
	require.True(t, synced)

	return slices.Clone(list.Items)
}

func (f frozenMembers) ListSynced(_ context.Context, list *metav1.PartialObjectMetadataList, _ ...client.ListOption) (bool, error) {
	list.Items = slices.Clone(f)

	return true, nil
}

func itemError(result *applyset.ApplyResult, id string) error {
	for _, item := range result.Applied {
		if item.ID == id {
			return item.Error
		}
	}

	return fmt.Errorf("no result for %s", id)
}
