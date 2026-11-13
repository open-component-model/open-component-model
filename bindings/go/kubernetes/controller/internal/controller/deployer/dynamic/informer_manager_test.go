package dynamic

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	toolscache "k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	configMapGVK = schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}
	secretGVK    = schema.GroupVersionKind{Version: "v1", Kind: "Secret"}
)

func TestInformerManagerSharesInformersAcrossParents(t *testing.T) {
	ctx := t.Context()
	r := require.New(t)
	fake := &fakeCache{informers: map[schema.GroupVersionKind]*fakeInformer{}}
	mgr := &InformerManager{cache: fake, tasks: map[schema.GroupVersionKind]*watchTask{}}

	first, second := parent("a", "deployer"), parent("b", "deployer")
	r.NoError(mgr.Register(ctx, first, child(configMapGVK, "a")))
	r.NoError(mgr.Register(ctx, second, child(configMapGVK, "b")))
	r.Equal(1, fake.gets[configMapGVK], "the cache runs one informer per GVK, whatever the namespace")
	r.True(mgr.HasSynced(first, child(configMapGVK, "a")))
	r.True(mgr.HasSynced(second, child(configMapGVK, "b")))

	r.NoError(mgr.Unregister(ctx, first, child(configMapGVK, "a")))
	r.Empty(mgr.ActiveForParent(first))
	r.True(mgr.IsStopped(first, child(configMapGVK, "a")))
	r.False(mgr.IsStopped(second, child(configMapGVK, "b")))
	r.True(mgr.HasSynced(second, child(configMapGVK, "b")), "another parent's unregister must not stop a shared informer")
	r.Zero(fake.removes[configMapGVK])

	r.NoError(mgr.Unregister(ctx, second, child(configMapGVK, "b")))
	r.Equal(1, fake.removes[configMapGVK], "the last parent stops the informer")
	r.Empty(mgr.tasks)
}

func TestInformerManagerActiveForParentIsPerInstance(t *testing.T) {
	ctx := t.Context()
	r := require.New(t)
	mgr := &InformerManager{
		cache: &fakeCache{informers: map[schema.GroupVersionKind]*fakeInformer{}},
		tasks: map[schema.GroupVersionKind]*watchTask{},
	}

	first, second := parent("ns", "first"), parent("ns", "second")
	r.NoError(mgr.Register(ctx, first, child(configMapGVK, "ns")))
	r.NoError(mgr.Register(ctx, second, child(secretGVK, "ns")))

	active := mgr.ActiveForParent(first)
	r.Len(active, 1, "a parent must only see its own watches, not those of every parent of its kind")
	r.Equal(configMapGVK, active[0].GetObjectKind().GroupVersionKind())
	r.False(mgr.HasSynced(first, child(secretGVK, "ns")), "a parent that never registered must register before relying on the watch")
}

func parent(namespace, name string) client.Object {
	return &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name}}
}

func child(gvk schema.GroupVersionKind, namespace string) client.Object {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)
	obj.SetNamespace(namespace)
	obj.SetName("child")

	return obj
}

type fakeCache struct {
	cache.Cache
	informers map[schema.GroupVersionKind]*fakeInformer
	gets      map[schema.GroupVersionKind]int
	removes   map[schema.GroupVersionKind]int
}

func (c *fakeCache) GetInformer(_ context.Context, obj client.Object, _ ...cache.InformerGetOption) (cache.Informer, error) {
	gvk := obj.GetObjectKind().GroupVersionKind()
	if c.gets == nil {
		c.gets = map[schema.GroupVersionKind]int{}
	}
	c.gets[gvk]++
	if _, ok := c.informers[gvk]; !ok {
		c.informers[gvk] = &fakeInformer{}
	}

	return c.informers[gvk], nil
}

func (c *fakeCache) RemoveInformer(_ context.Context, obj client.Object) error {
	if c.removes == nil {
		c.removes = map[schema.GroupVersionKind]int{}
	}
	c.removes[obj.GetObjectKind().GroupVersionKind()]++

	return nil
}

type fakeInformer struct {
	cache.Informer
}

func (*fakeInformer) AddEventHandlerWithOptions(toolscache.ResourceEventHandler, toolscache.HandlerOptions) (toolscache.ResourceEventHandlerRegistration, error) {
	return fakeRegistration{}, nil
}

func (*fakeInformer) RemoveEventHandler(toolscache.ResourceEventHandlerRegistration) error {
	return nil
}

func (*fakeInformer) HasSynced() bool { return true }

type fakeRegistration struct {
	toolscache.ResourceEventHandlerRegistration
}

func (fakeRegistration) HasSynced() bool { return true }
