package applyset

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const (
	// DefaultMemberSyncTimeout is how long a kind's informer may take to sync before the member cache drops it.
	DefaultMemberSyncTimeout = 30 * time.Second
	// DefaultMemberRetryAfter is how long the member cache leaves a dropped kind to the live list.
	DefaultMemberRetryAfter = 10 * time.Minute
	// DefaultMemberIdleTimeout is how long a kind's informer runs without a conflict check reading it.
	DefaultMemberIdleTimeout = 30 * time.Minute
)

// MemberLister lists ApplySet members from a cache, for Config.Members.
type MemberLister interface {
	// ListSynced lists the members into list and reports true once the cache holds their kind. Until then it
	// reports false without listing or waiting, and the caller lists live.
	ListSynced(ctx context.Context, list *metav1.PartialObjectMetadataList, opts ...client.ListOption) (bool, error)
}

// MemberCacheOptions configures NewMemberCache. Zero durations take the defaults.
type MemberCacheOptions struct {
	Cache       cache.Options
	SyncTimeout time.Duration
	RetryAfter  time.Duration
	IdleTimeout time.Duration
	Log         logr.Logger
}

// MemberCache keeps the name, namespace and part-of label of the ApplySet members of each kind conflict detection
// reads. A kind's first ListSynced starts its informer, which needs list and watch in all namespaces like drift
// detection. A kind that does not sync within SyncTimeout is retried after RetryAfter; one unread for IdleTimeout is
// removed.
type MemberCache struct {
	cache       cache.Cache
	syncTimeout time.Duration
	retryAfter  time.Duration
	idleTimeout time.Duration
	log         logr.Logger
	now         func() time.Time

	mu    sync.Mutex
	kinds map[schema.GroupVersionKind]*memberKind
}

type memberKind struct {
	started  time.Time
	lastUsed time.Time
	retryAt  time.Time
}

var (
	_ MemberLister                   = (*MemberCache)(nil)
	_ manager.Runnable               = (*MemberCache)(nil)
	_ manager.LeaderElectionRunnable = (*MemberCache)(nil)
)

// NewMemberCache creates a MemberCache. Add it to the manager to run it.
func NewMemberCache(cfg *rest.Config, opts MemberCacheOptions) (*MemberCache, error) {
	partOf, err := labels.NewRequirement(ApplysetPartOfLabel, selection.Exists, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build the ApplySet member selector: %w", err)
	}

	cacheOpts := opts.Cache
	cacheOpts.DefaultLabelSelector = labels.NewSelector().Add(*partOf)
	cacheOpts.DefaultTransform = keepMembership
	// detectConflicts only reads the members.
	cacheOpts.DefaultUnsafeDisableDeepCopy = ptr.To(true)
	// Only ListSynced starts informers, so a list never waits for one.
	cacheOpts.ReaderFailOnMissingInformer = true

	members, err := cache.New(cfg, cacheOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to create the ApplySet member cache: %w", err)
	}

	return newMemberCache(members, opts), nil
}

func newMemberCache(members cache.Cache, opts MemberCacheOptions) *MemberCache {
	c := &MemberCache{
		cache:       members,
		syncTimeout: opts.SyncTimeout,
		retryAfter:  opts.RetryAfter,
		idleTimeout: opts.IdleTimeout,
		log:         opts.Log,
		now:         time.Now,
		kinds:       make(map[schema.GroupVersionKind]*memberKind),
	}
	if c.syncTimeout <= 0 {
		c.syncTimeout = DefaultMemberSyncTimeout
	}
	if c.retryAfter <= 0 {
		c.retryAfter = DefaultMemberRetryAfter
	}
	if c.idleTimeout <= 0 {
		c.idleTimeout = DefaultMemberIdleTimeout
	}

	return c
}

// Start runs the cache and removes idle informers until ctx is done.
func (c *MemberCache) Start(ctx context.Context) error {
	go func() {
		ticker := time.NewTicker(c.idleTimeout / 2)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.removeIdle(ctx)
			}
		}
	}()

	if err := c.cache.Start(ctx); err != nil {
		return fmt.Errorf("failed to run the ApplySet member cache: %w", err)
	}

	return nil
}

// NeedLeaderElection reports false: only reconciles start informers, and they run on the leader.
func (c *MemberCache) NeedLeaderElection() bool {
	return false
}

func (c *MemberCache) ListSynced(ctx context.Context, list *metav1.PartialObjectMetadataList, opts ...client.ListOption) (bool, error) {
	gvk := list.GroupVersionKind()
	gvk.Kind = strings.TrimSuffix(gvk.Kind, "List")
	synced, err := c.synced(ctx, gvk)
	if err != nil || !synced {
		return false, err
	}

	// A removal racing this list would only fail it, as the cache does not start informers on a list.
	ctx, cancel := context.WithTimeout(ctx, c.syncTimeout)
	defer cancel()
	if err := c.cache.List(ctx, list, opts...); err != nil {
		return false, fmt.Errorf("failed to list cached ApplySet members: %w", err)
	}

	return true, nil
}

// synced reports whether the informer of gvk has synced, starting it if needed, and drops it when it has not
// synced within syncTimeout.
func (c *MemberCache) synced(ctx context.Context, gvk schema.GroupVersionKind) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	kind, ok := c.kinds[gvk]
	if !ok {
		kind = &memberKind{}
		c.kinds[gvk] = kind
	}
	kind.lastUsed = now
	if now.Before(kind.retryAt) {
		return false, nil
	}
	if kind.started.IsZero() {
		kind.started = now
	}

	informer, err := c.cache.GetInformer(ctx, partialObject(gvk), cache.BlockUntilSynced(false))
	if err != nil {
		return false, fmt.Errorf("failed to get the ApplySet member informer for %v: %w", gvk, err)
	}
	if informer.HasSynced() {
		return true, nil
	}

	if now.Sub(kind.started) >= c.syncTimeout {
		c.log.Info("dropping the ApplySet member informer that did not sync, which needs list and watch on the kind in all namespaces",
			"gvk", gvk.String(), "timeout", c.syncTimeout, "retryAfter", c.retryAfter)
		if err := c.cache.RemoveInformer(ctx, partialObject(gvk)); err != nil {
			return false, fmt.Errorf("failed to remove the ApplySet member informer for %v: %w", gvk, err)
		}
		kind.started = time.Time{}
		kind.retryAt = now.Add(c.retryAfter)
	}

	return false, nil
}

func (c *MemberCache) removeIdle(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	for gvk, kind := range c.kinds {
		if now.Sub(kind.lastUsed) < c.idleTimeout {
			continue
		}
		if err := c.cache.RemoveInformer(ctx, partialObject(gvk)); err != nil {
			c.log.Error(err, "failed to remove an idle ApplySet member informer", "gvk", gvk.String())
			continue
		}
		delete(c.kinds, gvk)
	}
}

func partialObject(gvk schema.GroupVersionKind) *metav1.PartialObjectMetadata {
	obj := &metav1.PartialObjectMetadata{}
	obj.SetGroupVersionKind(gvk)

	return obj
}

// keepMembership reduces a member to its membership, so the cache holds no managed fields, annotations or other
// labels.
func keepMembership(obj any) (any, error) {
	m, err := meta.Accessor(obj)
	if err != nil {
		return nil, fmt.Errorf("unable to access metadata: %w", err)
	}
	t, err := meta.TypeAccessor(obj)
	if err != nil {
		return nil, fmt.Errorf("unable to access type: %w", err)
	}

	member := membershipOf(m)

	return &metav1.PartialObjectMetadata{
		TypeMeta: metav1.TypeMeta{Kind: t.GetKind(), APIVersion: t.GetAPIVersion()},
		ObjectMeta: metav1.ObjectMeta{
			Name:            member.name,
			Namespace:       member.namespace,
			ResourceVersion: m.GetResourceVersion(),
			Labels:          map[string]string{ApplysetPartOfLabel: member.ownerID},
		},
	}, nil
}
