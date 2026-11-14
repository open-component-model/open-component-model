package applyset

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NewMemberCache returns a cache of the ApplySet members of every kind it lists, for Config.Reader. Its first list
// of a kind starts a metadata informer and waits for it to sync, so it needs list and watch on the applied kinds.
// Without them the informer never syncs, so a list fails after syncTimeout instead of blocking.
func NewMemberCache(cfg *rest.Config, syncTimeout time.Duration, opts cache.Options) (cache.Cache, error) {
	partOf, err := labels.NewRequirement(ApplysetPartOfLabel, selection.Exists, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build the ApplySet member selector: %w", err)
	}

	opts.DefaultLabelSelector = labels.NewSelector().Add(*partOf)
	opts.DefaultTransform = keepMembership
	// detectConflicts only reads the members.
	opts.DefaultUnsafeDisableDeepCopy = ptr.To(true)

	members, err := cache.New(cfg, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create the ApplySet member cache: %w", err)
	}

	return &memberCache{Cache: members, syncTimeout: syncTimeout}, nil
}

type memberCache struct {
	cache.Cache
	syncTimeout time.Duration
}

func (c *memberCache) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	ctx, cancel := context.WithTimeout(ctx, c.syncTimeout)
	defer cancel()

	if err := c.Cache.List(ctx, list, opts...); err != nil {
		return fmt.Errorf("failed to list ApplySet members, which needs list and watch on the kind: %w", err)
	}

	return nil
}

// keepMembership reduces a member to what detectConflicts reads, so the cache holds no managed fields, annotations
// or other labels.
func keepMembership(obj any) (any, error) {
	m, err := meta.Accessor(obj)
	if err != nil {
		return nil, fmt.Errorf("unable to access metadata: %w", err)
	}
	t, err := meta.TypeAccessor(obj)
	if err != nil {
		return nil, fmt.Errorf("unable to access type: %w", err)
	}

	return &metav1.PartialObjectMetadata{
		TypeMeta: metav1.TypeMeta{Kind: t.GetKind(), APIVersion: t.GetAPIVersion()},
		ObjectMeta: metav1.ObjectMeta{
			Name:            m.GetName(),
			Namespace:       m.GetNamespace(),
			ResourceVersion: m.GetResourceVersion(),
			Labels:          map[string]string{ApplysetPartOfLabel: m.GetLabels()[ApplysetPartOfLabel]},
		},
	}, nil
}
