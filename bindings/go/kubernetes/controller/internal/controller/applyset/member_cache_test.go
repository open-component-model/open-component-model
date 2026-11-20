package applyset

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TestKeepMembership_KeepsWhatConflictDetectionReads fails when conflict detection reads a field of a member that
// the member cache drops.
func TestKeepMembership_KeepsWhatConflictDetectionReads(t *testing.T) {
	r := require.New(t)

	member := &metav1.PartialObjectMetadata{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
		ObjectMeta: metav1.ObjectMeta{
			Name:            "cm",
			Namespace:       "default",
			UID:             "uid",
			ResourceVersion: "7",
			Labels:          map[string]string{ApplysetPartOfLabel: "applyset-other-v1", "app": "other"},
			Annotations:     map[string]string{"note": "dropped"},
			ManagedFields:   []metav1.ManagedFieldsEntry{{Manager: "kubectl"}},
		},
	}

	kept, err := keepMembership(member)
	r.NoError(err)
	reduced, ok := kept.(*metav1.PartialObjectMetadata)
	r.True(ok)

	r.Equal(membershipOf(member), membershipOf(reduced), "the cache must keep everything conflict detection reads")
	r.Equal(map[string]string{ApplysetPartOfLabel: "applyset-other-v1"}, reduced.GetLabels())
	r.Empty(reduced.GetAnnotations())
	r.Empty(reduced.GetManagedFields())
}

func TestMemberCache_ListSynced(t *testing.T) {
	t.Run("lists a synced kind from the cache", func(t *testing.T) {
		r := require.New(t)
		informers := &fakeInformers{synced: true}
		members, _ := newTestMemberCache(informers)

		synced, err := members.ListSynced(t.Context(), configMapMembers())
		r.NoError(err)
		r.True(synced)
		r.Equal(1, informers.lists)
	})

	t.Run("leaves a syncing kind to the live list without waiting", func(t *testing.T) {
		r := require.New(t)
		informers := &fakeInformers{}
		members, _ := newTestMemberCache(informers)

		synced, err := members.ListSynced(t.Context(), configMapMembers())
		r.NoError(err)
		r.False(synced)
		r.Zero(informers.lists)
		r.Equal(1, informers.gets, "the first check starts the informer")
		r.Empty(informers.removed)
	})

	t.Run("drops a kind that does not sync and retries it later", func(t *testing.T) {
		r := require.New(t)
		informers := &fakeInformers{}
		members, clock := newTestMemberCache(informers)

		check := func() {
			synced, err := members.ListSynced(t.Context(), configMapMembers())
			r.NoError(err)
			r.False(synced)
		}

		check()
		*clock = clock.Add(members.syncTimeout)
		check()
		r.Equal([]schema.GroupVersionKind{configMapGVK}, informers.removed)

		*clock = clock.Add(members.retryAfter - time.Second)
		check()
		r.Equal(2, informers.gets, "a dropped kind is not restarted before retryAfter")

		*clock = clock.Add(time.Second)
		informers.synced = true
		synced, err := members.ListSynced(t.Context(), configMapMembers())
		r.NoError(err)
		r.True(synced)
		r.Equal(3, informers.gets)
	})
}

func TestMemberCache_RemovesIdleInformers(t *testing.T) {
	r := require.New(t)
	informers := &fakeInformers{synced: true}
	members, clock := newTestMemberCache(informers)

	_, err := members.ListSynced(t.Context(), configMapMembers())
	r.NoError(err)

	*clock = clock.Add(members.idleTimeout - time.Second)
	members.removeIdle(t.Context())
	r.Empty(informers.removed)

	*clock = clock.Add(time.Second)
	members.removeIdle(t.Context())
	r.Equal([]schema.GroupVersionKind{configMapGVK}, informers.removed)
	r.Empty(members.kinds)
}

var configMapGVK = schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}

func newTestMemberCache(informers cache.Cache) (*MemberCache, *time.Time) {
	members := newMemberCache(informers, MemberCacheOptions{})
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	members.now = func() time.Time { return clock }

	return members, &clock
}

func configMapMembers() *metav1.PartialObjectMetadataList {
	list := &metav1.PartialObjectMetadataList{}
	list.SetGroupVersionKind(configMapGVK.GroupVersion().WithKind("ConfigMapList"))

	return list
}

// fakeInformers stands in for an informer cache whose informers have or have not synced.
type fakeInformers struct {
	cache.Cache
	synced  bool
	gets    int
	lists   int
	removed []schema.GroupVersionKind
}

func (c *fakeInformers) GetInformer(context.Context, client.Object, ...cache.InformerGetOption) (cache.Informer, error) {
	c.gets++

	return fakeInformer{synced: c.synced}, nil
}

func (c *fakeInformers) RemoveInformer(_ context.Context, obj client.Object) error {
	c.removed = append(c.removed, obj.GetObjectKind().GroupVersionKind())

	return nil
}

func (c *fakeInformers) List(context.Context, client.ObjectList, ...client.ListOption) error {
	c.lists++

	return nil
}

type fakeInformer struct {
	cache.Informer
	synced bool
}

func (i fakeInformer) HasSynced() bool {
	return i.synced
}
