package resolution

import (
	"context"
	"fmt"
	"time"
)

// Start begins the background worker pool.
func (r *Resolver) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.logger.Info("starting component version resolver", "workers", r.opts.WorkerCount, "queueSize", r.opts.QueueSize)

	for i := range r.opts.WorkerCount {
		go r.workerWithRecovery(ctx, i)
	}

	return nil
}

// workerWithRecovery wraps the worker with panic recovery and auto-restart.
func (r *Resolver) workerWithRecovery(ctx context.Context, id int) {
	logger := r.logger.WithValues("worker", id)

	for {
		select {
		case <-ctx.Done():
			logger.V(1).Info("worker stopped due to context cancellation")
			return
		default:
			// run worker with panic recovery
			func() {
				defer func() {
					if rec := recover(); rec != nil {
						logger.Error(fmt.Errorf("worker panic: %v", rec), "worker panicked, restarting")
						// let the worker restart
					}
				}()

				r.worker(ctx, id)
			}()

			// worker exited normally (context cancelled)
			return
		}
	}
}

// worker processes lookup requests from the queue.
func (r *Resolver) worker(ctx context.Context, id int) {
	logger := r.logger.WithValues("worker", id)
	logger.V(1).Info("worker started")

	for {
		select {
		case <-ctx.Done():
			logger.V(1).Info("worker stopped")
			return
		case req := <-r.lookupQueue:
			QueueSizeGauge.Set(float64(len(r.lookupQueue)))

			logger.V(1).Info("processing lookup request", "component", req.opts.Component, "version", req.opts.Version)

			start := time.Now()
			result, err := r.resolve(req.ctx, req.opts, req.cfg)
			duration := time.Since(start).Seconds()

			ResolutionDurationHistogram.WithLabelValues(req.opts.Component, req.opts.Version).Observe(duration)

			r.mu.Lock()
			if err != nil {
				logger.Error(err, "failed to resolve component version", "component", req.opts.Component, "version", req.opts.Version, "duration", duration)
				r.cache[req.key.String()] = &ResolveResult{
					Error: err,
					Metadata: ResolveMetadata{
						ResolvedAt: time.Now(),
						ConfigHash: req.cfg.Hash,
					},
				}
			} else {
				r.cache[req.key.String()] = result
				logger.V(1).Info("cached component version", "component", req.opts.Component, "version", req.opts.Version, "duration", duration)
			}

			delete(r.inProgress, req.key.String())
			InProgressGauge.Set(float64(len(r.inProgress)))
			r.mu.Unlock()
		}
	}
}
