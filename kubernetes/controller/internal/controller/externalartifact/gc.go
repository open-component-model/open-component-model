package externalartifact

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
)

// GarbageCollector periodically removes on-disk artifact directories whose
// owning Resource no longer exists.
//
// The live delete path (finalizer + reconcileDelete) already removes a
// Resource's artifacts when it is deleted while the controller is running. This
// sweep closes the crash-window gap: if a Resource is deleted while the
// controller is down, its finalizer logic never runs, so the artifact directory
// would otherwise leak on disk forever. On a schedule, the collector lists the
// artifact directories on disk, checks whether the corresponding Resource still
// exists in the cluster, and removes the directory if it does not.
//
// It is leader-elected: only one replica performs deletions, avoiding races on
// a shared ReadWriteMany volume.
type GarbageCollector struct {
	Client   client.Client
	Storage  *Storage
	Interval time.Duration
	Metrics  *Metrics
}

var (
	_ manager.Runnable               = (*GarbageCollector)(nil)
	_ manager.LeaderElectionRunnable = (*GarbageCollector)(nil)
)

// NewGarbageCollector builds a GarbageCollector that sweeps every interval.
func NewGarbageCollector(c client.Client, storage *Storage, interval time.Duration, metrics *Metrics) *GarbageCollector {
	return &GarbageCollector{
		Client:   c,
		Storage:  storage,
		Interval: interval,
		Metrics:  metrics,
	}
}

// NeedLeaderElection returns true: only the leader deletes artifacts, so that
// replicas sharing a ReadWriteMany volume do not race on removals.
func (g *GarbageCollector) NeedLeaderElection() bool {
	return true
}

// Start runs the sweep loop until the context is cancelled. It performs an
// initial sweep shortly after startup (to reclaim anything orphaned while the
// controller was down), then on the configured interval.
func (g *GarbageCollector) Start(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName("external-artifact-gc")
	logger.Info("starting external artifact garbage collector", "interval", g.Interval.String())

	ticker := time.NewTicker(g.Interval)
	defer ticker.Stop()

	// Initial sweep: don't wait a full interval to reclaim crash-window orphans.
	if err := g.runSweep(ctx); err != nil {
		logger.Error(err, "initial artifact garbage-collection sweep failed")
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := g.runSweep(ctx); err != nil {
				logger.Error(err, "artifact garbage-collection sweep failed")
			}
		}
	}
}

// runSweep times a sweep and records its duration.
func (g *GarbageCollector) runSweep(ctx context.Context) error {
	start := time.Now()
	err := g.sweep(ctx)
	if g.Metrics != nil {
		g.Metrics.SweepDurationSeconds.Observe(time.Since(start).Seconds())
	}

	return err
}

// sweep lists on-disk artifact objects and removes those whose owning Resource
// no longer exists in the cluster. Only Resource-kind artifacts are considered;
// unknown kinds are left untouched so the collector never deletes data it does
// not understand. It also verifies artifact integrity and refreshes
// disk-usage metrics.
func (g *GarbageCollector) sweep(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName("external-artifact-gc")

	objects, err := g.Storage.ListArtifactObjects()
	if err != nil {
		g.recordPruneError()

		return fmt.Errorf("failed to list artifact objects: %w", err)
	}

	var removed, kept int
	for _, obj := range objects {
		// Only manage what we produce: Resource artifacts. ArtifactDir lowercases
		// the kind, so compare against the lowercased known kind.
		if obj.Kind != strings.ToLower(v1alpha1.KindResource) {
			continue
		}

		exists, err := g.resourceExists(ctx, obj.Namespace, obj.Name)
		if err != nil {
			// On a transient API error, keep the artifact and try again next sweep.
			logger.Error(err, "failed to check resource existence, keeping artifact",
				"namespace", obj.Namespace, "name", obj.Name)

			continue
		}
		if exists {
			kept++

			continue
		}

		// Orphan: the Resource is gone but its artifacts remain on disk.
		if err := g.Storage.RemoveAll(v1alpha1.KindResource, obj.Namespace, obj.Name); err != nil {
			g.recordPruneError()
			logger.Error(err, "failed to remove orphaned artifact",
				"namespace", obj.Namespace, "name", obj.Name)

			continue
		}
		removed++
		logger.Info("removed orphaned artifact", "namespace", obj.Namespace, "name", obj.Name)
	}

	// Verify integrity of the artifacts that remain: re-hash each and remove any
	// whose content no longer matches its digest-addressed filename. The
	// reconciler repackages a clean copy on the next reconcile.
	if corrupt, err := g.Storage.VerifyIntegrity(); err != nil {
		logger.Error(err, "artifact integrity verification failed")
	} else if corrupt > 0 {
		g.recordIntegrityFailures(corrupt)
		logger.Info("removed corrupt artifacts; will be repackaged on next reconcile", "count", corrupt)
	}

	// Refresh disk-usage gauges.
	g.refreshUsageMetrics(logger)

	if removed > 0 {
		logger.Info("artifact garbage-collection sweep complete", "removed", removed, "kept", kept)
	}

	return nil
}

// refreshUsageMetrics recomputes and publishes the disk-usage gauges.
func (g *GarbageCollector) refreshUsageMetrics(logger logr.Logger) {
	if g.Metrics == nil {
		return
	}
	bytes, count, err := g.Storage.DiskUsage()
	if err != nil {
		logger.Error(err, "failed to compute disk usage")

		return
	}
	g.Metrics.DiskUsedBytes.Set(float64(bytes))
	g.Metrics.ArtifactCount.Set(float64(count))
}

// resourceExists reports whether the Resource identified by namespace/name
// still exists in the cluster.
func (g *GarbageCollector) resourceExists(ctx context.Context, namespace, name string) (bool, error) {
	resource := &v1alpha1.Resource{}
	err := g.Client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, resource)
	switch {
	case err == nil:
		return true, nil
	case apierrors.IsNotFound(err):
		return false, nil
	default:
		return false, err
	}
}

func (g *GarbageCollector) recordPruneError() {
	if g.Metrics != nil {
		g.Metrics.PruneErrorsTotal.Inc()
	}
}

func (g *GarbageCollector) recordIntegrityFailures(n int) {
	if g.Metrics != nil {
		g.Metrics.IntegrityFailuresTotal.Add(float64(n))
	}
}
