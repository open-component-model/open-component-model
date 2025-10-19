package resolution

import (
	"context"
	"sync"
	"time"
)

// Start begins the background doWork pool and result collector.
func (r *Resolver) Start(ctx context.Context) error {
	r.logger.Info("starting component version resolver", "workers", r.opts.WorkerCount, "queueSize", r.opts.QueueSize)

	// start workers and collect their result channels( fan-out )
	workerChannels := make([]chan *Result, 0, r.opts.WorkerCount)
	for i := range r.opts.WorkerCount {
		workerChannels = append(workerChannels, r.startWorker(ctx, i))
	}

	go r.resultCollector(ctx, workerChannels)

	return nil
}

// resultCollector processes results from multiple doWork channels and updates the cache and inProgress map.
func (r *Resolver) resultCollector(ctx context.Context, workerChannels []chan *Result) {
	logger := r.logger.WithValues("component", "result-collector")
	logger.V(1).Info("result collector started", "workerCount", len(workerChannels))

	// merge all worker channels into a single channel and set the result in the cache ( fan-in )
	mergedResults := make(chan *Result)
	wg := &sync.WaitGroup{}
	wg.Add(len(workerChannels))
	for _, ch := range workerChannels {
		go func() {
			for res := range ch {
				mergedResults <- res
			}
			wg.Done()
		}()
	}

	go func() {
		// close the channel if all workers exit so the collector may exit as well
		wg.Wait()
		close(mergedResults)
	}()

	for {
		select {
		case <-ctx.Done():
			logger.V(1).Info("result collector stopped")
			return
		case res := <-mergedResults:
			r.cache.Set(res.key, res)

			// clear singleflight to prevent memory leak
			r.sf.Forget(res.key)

			// mark the work as done in the progress tracker
			r.inProgress.Delete(res.key)
			//InProgressGauge.Set(float64(len(r.inProgress)))
		default:
		}
	}
}

// startWorker creates a doWork's result channel and starts the doWork in a goroutine.
// Returns the channel that the doWork will send results to.
func (r *Resolver) startWorker(ctx context.Context, id int) chan *Result {
	resultChan := make(chan *Result)
	logger := r.logger.WithValues("doWork", id)

	go func() {
		defer close(resultChan)
		defer logger.V(1).Info("worker stopped completely")

		for {
			select {
			case <-ctx.Done():
				logger.V(1).Info("doWork stopped due to context cancellation")
				return
			case req := <-r.lookupQueue:
				QueueSizeGauge.Set(float64(len(r.lookupQueue)))

				logger.V(1).Info("processing lookup request", "component", req.opts.Component, "version", req.opts.Version)

				start := time.Now()
				// we are using the passed in context for the resolve operation.
				result, err := r.resolve(req.ctx, &req.opts, &req.cfg)
				duration := time.Since(start).Seconds()

				ResolutionDurationHistogram.WithLabelValues(req.opts.Component, req.opts.Version).Observe(duration)

				if err != nil {
					logger.Error(err, "failed to resolve component version", "component", req.opts.Component, "version", req.opts.Version, "duration", duration)
				} else {
					logger.V(1).Info("resolved component version", "component", req.opts.Component, "version", req.opts.Version, "duration", duration)
				}

				// Send result to doWork's dedicated result channel
				resultChan <- &Result{
					key:    req.key,
					result: result,
					err:    err,
				}
			default:
			}
		}
	}()

	return resultChan
}
