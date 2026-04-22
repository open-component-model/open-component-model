package workerpool

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/signinghandler"
	"ocm.software/open-component-model/bindings/go/repository"
	signingv1alpha1 "ocm.software/open-component-model/bindings/go/rsa/signing/v1alpha1"
	"ocm.software/open-component-model/bindings/go/signing"
	"ocm.software/open-component-model/kubernetes/controller/internal/verification"
)

// RequesterInfo contains information about the object requesting resolution.
type RequesterInfo struct {
	NamespacedName types.NamespacedName
}

// ErrNotSafelyDigestible is a sentinel error used to identify this error type.
var ErrNotSafelyDigestible = errors.New("not safely digestible")

// NotSafelyDigestibleError contains information about a component version that is not safely digestible.
type NotSafelyDigestibleError struct {
	Component string
	Version   string
	Err       error
}

func (e *NotSafelyDigestibleError) Error() string {
	return fmt.Sprintf("component version %s:%s is not safely digestible: %v", e.Component, e.Version, e.Err)
}

func (e *NotSafelyDigestibleError) Unwrap() error {
	return ErrNotSafelyDigestible
}

// NewNotSafelyDigestibleError creates a new NotSafelyDigestibleError with component and version information.
func NewNotSafelyDigestibleError(component, version string, err error) *NotSafelyDigestibleError {
	return &NotSafelyDigestibleError{
		Component: component,
		Version:   version,
		Err:       err,
	}
}

// ResolveOptions contains all the options the resolution service requires to perform a resolve operation.
type ResolveOptions struct {
	Component  string
	Version    string
	Repository repository.ComponentVersionRepository
	// Verifications are used to verify against component version signatures and used a cache key.
	Verifications []verification.Verification
	// Digest is used to verify the integrity of a referenced component version and is used as part of the cache key.
	Digest          *v2.Digest
	SigningRegistry *signinghandler.SigningRegistry
	KeyFunc         func() (string, error)
	// Requester is the information about the object requesting this resolution.
	// It will be notified when the resolution completes.
	Requester RequesterInfo
}

// Result contains the result of a resolution including any errors that might have occurred.
type Result struct {
	Value any
	Error error
}

// WorkItem represents a single work item to be processed by the worker pool.
type WorkItem struct {
	// Fn is the work function that is executed to process a work item.
	Fn workFunc
	// Opts contains the resolve options.
	Opts ResolveOptions
	// key is the calculated key that is passed in from the top to avoid
	// the error handling from the key function later.
	key string
}

// PoolOptions configures the worker pool.
type PoolOptions struct {
	// WorkerCount is the number of concurrent workers.
	WorkerCount int
	// QueueSize is the size of the work queue buffer.
	QueueSize int
	// SubscriberBufferSize is the buffer size for each subscriber's event channel.
	// A larger buffer reduces the probability of dropped events under load.
	SubscriberBufferSize int
	// Logger for the worker pool.
	Logger *logr.Logger
	// Client for Kubernetes API access.
	Client client.Reader
	// Cache for caching.
	Cache *expirable.LRU[string, *Result]
}

// WorkerPool manages a pool of workers that process work items concurrently.
type WorkerPool struct {
	PoolOptions
	workQueue     chan *WorkItem
	inProgressMu  sync.Mutex
	subscribersMu sync.RWMutex
	subscribers   []chan []RequesterInfo
	// tracks all requesters per resolution key to make sure that all objects who request this item will
	// be notified of any change.
	inProgress  map[string][]RequesterInfo
	workersDone sync.WaitGroup
}

// ErrResolutionInProgress is returned when a component version is being resolved in the background.
var ErrResolutionInProgress = fmt.Errorf("component version resolution in progress")

// NewWorkerPool creates a new worker pool.
func NewWorkerPool(opts PoolOptions) *WorkerPool {
	if opts.WorkerCount <= 0 {
		opts.WorkerCount = 10
	}

	if opts.QueueSize <= 0 {
		opts.QueueSize = 1000
	}

	if opts.SubscriberBufferSize <= 0 {
		opts.SubscriberBufferSize = 100
	}

	return &WorkerPool{
		PoolOptions: opts,
		workQueue:   make(chan *WorkItem, opts.QueueSize),
		inProgress:  make(map[string][]RequesterInfo),
		subscribers: make([]chan []RequesterInfo, 0),
	}
}

// Subscribe creates a new event subscription channel and registers it to receive
// resolution events. Each subscriber gets its own buffered channel to avoid events being
// consumed by only one listener and controllers stealing events from other controllers.
// The channel is buffered to prevent blocking workers. If the buffer fills, events are dropped.
// The returned channel will be closed when the worker pool shuts down.
func (wp *WorkerPool) Subscribe() <-chan []RequesterInfo {
	wp.subscribersMu.Lock()
	defer wp.subscribersMu.Unlock()

	ch := make(chan []RequesterInfo, wp.SubscriberBufferSize)
	wp.subscribers = append(wp.subscribers, ch)
	return ch
}

// Start begins the worker pool.
// This method blocks until the context is canceled to implement graceful shutdown.
func (wp *WorkerPool) Start(ctx context.Context) error {
	wp.Logger.Info("starting worker pool", "workers", wp.WorkerCount, "queueSize", wp.QueueSize, "subscriberBufferSize", wp.SubscriberBufferSize)

	for i := range wp.WorkerCount {
		wp.workersDone.Add(1)
		go wp.worker(ctx, i)
	}

	// wait for context cancellation
	<-ctx.Done()
	wp.Logger.Info("worker pool shutting down, draining queue")

	// wait for all workers to finish
	done := make(chan struct{})
	go func() {
		wp.workersDone.Wait()

		// now it's safe to close the channels
		close(wp.workQueue)

		wp.subscribersMu.Lock()
		for _, ch := range wp.subscribers {
			close(ch)
		}
		wp.subscribersMu.Unlock()

		close(done)
	}()

	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	select {
	case <-done:
		wp.Logger.Info("worker pool shutdown complete")
		return nil
	case <-timeout.C:
		return fmt.Errorf("timed out waiting for worker pool to shutdown")
	}
}

// GetComponentVersion retrieves a component version using the worker pool and cache.
func (wp *WorkerPool) GetComponentVersion(ctx context.Context, opts ResolveOptions) (*descriptor.Descriptor, error) {
	return resolveWorkRequest[*descriptor.Descriptor](ctx, wp, opts, wp.getComponentVersion)
}

// resolveWorkRequest is an abstraction in front of the worker queue and resolution logic. It is meant to be called by
// small purpose functions, like the GetComponentVersion function above, that wish to use the worker-pool to cache results.
// For example, another function could be GetLocalResource that caches the blob object.
func resolveWorkRequest[T any](ctx context.Context, wp *WorkerPool, opts ResolveOptions, fn workFunc) (result T, _ error) {
	wp.inProgressMu.Lock()
	defer wp.inProgressMu.Unlock()

	key, err := opts.KeyFunc()
	if err != nil {
		return result, fmt.Errorf("failed to generate cache key: %w", err)
	}

	// Check cache BEFORE checking in-progress, otherwise we get into a scenario where
	// cache has been populated but in-progress has not yet been cleared and an error
	// is returned even though the value exists.
	// This is a slim chance, but not zero.
	// handleWorkItem -> Cache.Add
	// resolveWorkRequest -> locks InProgress so handleWorkItem cannot lock to delete the key
	// If it would check InProgress before we check the cache it would return the error even though the item
	// is already in the cache.
	// With this, it returns, releases in-progress mutex, defer in handleWorkItem continues and removes the
	// InProgress key.
	if cached, ok := wp.Cache.Get(key); ok {
		CacheHitCounterTotal.WithLabelValues(opts.Component, opts.Version, verificationState(opts.Verifications, opts.Digest)).Inc()
		// In case of an error of type ErrNotSafelyDigestible we return the cached error and value because we want
		// to pass through the information that this component version is not safely digestible to the controller
		// but still use the value.
		if errors.Is(cached.Error, ErrNotSafelyDigestible) {
			res, ok := cached.Value.(T)
			if !ok {
				return result, fmt.Errorf("unable to assert cache value for key %s into requested type, was: %T", key, cached.Value)
			}

			return res, cached.Error
		}

		if cached.Error != nil {
			// we remove error results from the cache, so the controller can immediately retry.
			wp.Cache.Remove(key)
			return result, cached.Error
		}

		res, ok := cached.Value.(T)
		if !ok {
			return result, fmt.Errorf("unable to assert cache value for key %s into requested type, was: %T", key, cached.Value)
		}

		return res, nil
	}

	CacheMissCounterTotal.WithLabelValues(opts.Component, opts.Version, verificationState(opts.Verifications, opts.Digest)).Inc()

	// check if already/still in progress
	if requesters, exists := wp.inProgress[key]; exists {
		// add this requester to the list if not already present (deduplicate)
		alreadyRequested := false
		for _, r := range requesters {
			if r.NamespacedName == opts.Requester.NamespacedName {
				alreadyRequested = true
				break
			}
		}
		if !alreadyRequested {
			wp.inProgress[key] = append(requesters, opts.Requester)
			wp.Logger.V(1).Info("resolution still in progress, added requester",
				"component", opts.Component,
				"version", opts.Version,
				"requester", opts.Requester.NamespacedName)
		} else {
			wp.Logger.V(1).Info("resolution still in progress, requester already tracked",
				"component", opts.Component,
				"version", opts.Version,
				"requester", opts.Requester.NamespacedName)
		}
		return result, ErrResolutionInProgress
	}

	// check for context cancellation before enqueuing
	select {
	case <-ctx.Done():
		return result, ctx.Err()
	default:
	}

	workItem := &WorkItem{
		Fn:   fn,
		Opts: opts,
		key:  key,
	}

	select {
	case wp.workQueue <- workItem:
		// first requester
		wp.inProgress[key] = []RequesterInfo{opts.Requester}
		InProgressGauge.Set(float64(len(wp.inProgress)))
		QueueSizeGauge.Set(float64(len(wp.workQueue)))
		wp.Logger.V(1).Info("enqueued request", "component", opts.Component, "requester", opts.Requester.NamespacedName)

		return result, ErrResolutionInProgress
	default:
		if len(wp.workQueue) == wp.QueueSize {
			return result, fmt.Errorf("work queue is full; cannot resolve requests for %s", opts.Component)
		}

		return result, fmt.Errorf("cannot enqueue request for %s", opts.Component)
	}
}

// worker is the main worker loop that processes work items and updates the cache directly.
func (wp *WorkerPool) worker(ctx context.Context, id int) {
	defer wp.workersDone.Done()
	logger := wp.Logger.WithValues("worker", id)
	defer logger.V(1).Info("worker stopped")

	for {
		select {
		case <-ctx.Done():
			logger.V(1).Info("worker stopped due to context cancellation")
			return
		case item := <-wp.workQueue:
			QueueSizeGauge.Set(float64(len(wp.workQueue)))
			wp.handleWorkItem(ctx, &logger, item)
		}
	}
}

// workFunc is the signature for functions that process work items.
type workFunc func(ctx context.Context, item ResolveOptions) (any, error)

func (wp *WorkerPool) handleWorkItem(ctx context.Context, logger *logr.Logger, item *WorkItem) {
	logger.V(1).Info("processing work item", "key", item.key)

	start := time.Now()
	result, err := item.Fn(ctx, item.Opts)
	duration := time.Since(start).Seconds()

	// Track metrics
	ResolutionDurationHistogram.WithLabelValues(item.Opts.Component, item.Opts.Version, verificationState(item.Opts.Verifications, item.Opts.Digest)).Observe(duration)

	if err != nil {
		logger.Error(err, "failed to process work item",
			"component", item.Opts.Component,
			"version", item.Opts.Version,
			"duration", duration)
	} else {
		logger.V(1).Info("processed work item",
			"component", item.Opts.Component,
			"version", item.Opts.Version,
			"duration", duration)
	}

	// get all requesters AFTER resolution completes but BEFORE cleanup
	// ensures we capture all requesters that were added during the resolution and the wait for it to be finished
	requesters := wp.setResult(item.key, result, err)

	// notify all subscribers of an event happening.
	// Uses buffered channels with non-blocking send to avoid worker goroutine overhead.
	wp.subscribersMu.RLock()
	subscribers := slices.Clone(wp.subscribers)
	wp.subscribersMu.RUnlock()

	for _, ch := range subscribers {
		select {
		case <-ctx.Done():
			logger.V(1).Info("context canceled, skipping event broadcast",
				"component", item.Opts.Component,
				"version", item.Opts.Version)
			return
		case ch <- requesters:
			logger.V(1).Info("sent resolution event to subscriber",
				"component", item.Opts.Component,
				"version", item.Opts.Version,
				"requesterCount", len(requesters))
		default:
			logger.Info("dropped resolution event, subscriber buffer full",
				"component", item.Opts.Component,
				"version", item.Opts.Version)
			EventChannelDropsTotal.WithLabelValues(item.Opts.Component, item.Opts.Version, verificationState(item.Opts.Verifications, item.Opts.Digest)).Inc()
		}
	}
}

func (wp *WorkerPool) setResult(key string, result any, err error) []RequesterInfo {
	wp.inProgressMu.Lock()
	defer wp.inProgressMu.Unlock()

	wp.Cache.Add(key, &Result{
		Value: result,
		Error: err,
	})

	requesters := slices.Clone(wp.inProgress[key])
	delete(wp.inProgress, key)
	InProgressGauge.Set(float64(len(wp.inProgress)))
	return requesters
}

// getComponentVersion performs the actual component version resolution. If verifications or a digest from a component
// reference from a parent component are provided, it performs the necessary integrity and signature verification.
func (wp *WorkerPool) getComponentVersion(ctx context.Context, opts ResolveOptions) (any, error) {
	logger := log.FromContext(ctx)

	desc, err := opts.Repository.GetComponentVersion(ctx, opts.Component, opts.Version)
	if err != nil {
		return nil, fmt.Errorf("failed to get component version %s:%s: %w", opts.Component, opts.Version, err)
	}

	if opts.Digest != nil && len(opts.Verifications) > 0 {
		return nil, fmt.Errorf(
			"invalid resolve options for %s:%s: digest and verifications are mutually exclusive",
			opts.Component, opts.Version,
		)
	}

	switch {
	case opts.Digest != nil:
		return compareDigest(ctx, desc, opts.Digest)
	case len(opts.Verifications) > 0:
		// If verifications are requested, we need to verify that the component version is safely digestible.
		// Anything that comes after this will, in case of an error, always be skipped until cache TTL expires
		if err := signing.IsSafelyDigestible(&desc.Component); err != nil {
			return desc, fmt.Errorf("%w: %w", ErrNotSafelyDigestible, err)
		}

		if opts.SigningRegistry == nil {
			return nil, fmt.Errorf("signing registry is required when verifications are configured")
		}

		return verifySignatures(ctx, desc, opts.Verifications, opts.SigningRegistry)
	default:
		logger.Info("no digest or verifications provided, skipping integrity and signature verification",
			"component", opts.Component, "version", opts.Version)
		return desc, nil
	}
}

// verifySignatures performs signature verification for the provided component version descriptor and the list of
// verifications.
func verifySignatures(ctx context.Context, desc *descriptor.Descriptor, verifications []verification.Verification, signingRegistry *signinghandler.SigningRegistry) (*descriptor.Descriptor, error) {
	logger := log.FromContext(ctx)
	logger.Info("verifying signature", "component", desc.Component.Name, "version", desc.Component.Version)

	signingHandler, err := signingRegistry.GetPlugin(ctx, &signingv1alpha1.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to get signing handler plugin: %w", err)
	}

	for _, v := range verifications {
		var descSig *descriptor.Signature
		for i := range desc.Signatures {
			if desc.Signatures[i].Name == v.Signature {
				descSig = &desc.Signatures[i]
				break
			}
		}

		if descSig == nil {
			return nil, fmt.Errorf("signature %s not found in component %s", v.Signature, desc.Component.Name)
		}

		if err := signing.VerifyDigestMatchesDescriptor(ctx, desc, *descSig, slog.New(logr.ToSlogHandler(logger))); err != nil {
			return nil, fmt.Errorf("digest v failed for signature %q: %w", descSig.Name, err)
		}

		// TODO: We need to derive the expected credential key from the signature algorithm. This does not look that
		//       reliable currently. This will probably change, when typed credentials are supported.
		credentials := map[string]string{}
		switch signingv1alpha1.SignatureAlgorithm(descSig.Signature.Algorithm) {
		case signingv1alpha1.AlgorithmRSASSAPSS, signingv1alpha1.AlgorithmRSASSAPKCS1V15:
			credentials["public_key_pem"] = string(v.PublicKey)
		default:
			return nil, fmt.Errorf("unsupported signature algorithm: %q", descSig.Signature.Algorithm)
		}

		if err := signingHandler.Verify(ctx, *descSig, &signingv1alpha1.Config{}, credentials); err != nil {
			return nil, fmt.Errorf("signature v failed for signature %s: %w", v.Signature, err)
		}
	}

	return desc, nil
}

// compareDigest performs integrity verification using the provided digest against a fresh calculated digest of
// the passed descriptor.
func compareDigest(ctx context.Context, desc *descriptor.Descriptor, digest *v2.Digest) (*descriptor.Descriptor, error) {
	logger := log.FromContext(ctx)

	logger.Info("verifying integrity with provided digest",
		"component", desc.Component.Name, "version", desc.Component.Version)

	digestDesc, err := signing.GenerateDigest(ctx, desc, slog.New(logr.ToSlogHandler(logger)),
		digest.NormalisationAlgorithm, digest.HashAlgorithm)
	if err != nil {
		return nil, fmt.Errorf("failed to generate digest for component version %s:%s: %w",
			desc.Component.Name, desc.Component.Version, err)
	}

	if digestDesc.Value != digest.Value {
		return nil, fmt.Errorf("digest mismatch (%s/%s) for component version %s:%s: expected %s, got %s",
			digest.NormalisationAlgorithm, digest.HashAlgorithm, desc.Component.Name, desc.Component.Version,
			digest.Value, digestDesc.Value)
	}

	return desc, nil
}

func verificationState(verifications []verification.Verification, digest *v2.Digest) string {
	hasVerifications := len(verifications) != 0
	hasDigest := digest != nil

	switch {
	case hasVerifications && hasDigest:
		return "unknown"
	case hasVerifications || hasDigest:
		return "verified"
	default:
		return "unverified"
	}
}
