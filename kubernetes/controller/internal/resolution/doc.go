// Package resolution provides async, cache-backed component version fetching with optional
// signature verification and digest integrity checking.
//
// # Usage
//
// Create a [Resolver] during controller setup using [NewResolver]. The worker pool must be
// added to the controller manager separately so it starts and stops with the manager lifecycle.
//
//	wp := workerpool.New(...)
//	resolver := resolution.NewResolver(client, logger, wp, pluginManager)
//	mgr.Add(wp)
//
// To fetch a component version, create a [CacheBackedRepository] via [Resolver.NewCacheBackedRepository]
// with [RepositoryOptions], then call GetComponentVersion on it:
//
//	repo, err := resolver.NewCacheBackedRepository(ctx, &resolution.RepositoryOptions{
//	    RepositorySpec:    repoSpec,
//	    OCMConfigurations: configs,
//	    Namespace:         obj.GetNamespace(),
//	    SigningRegistry:   signingRegistry,     // required if Verifications is set
//	    Verifications:     verifications,        // optional: signature verification
//	    Digest:            digest,               // optional: integrity check for referenced components
//	    RequesterFunc:     requesterFunc,        // identifies the requesting controller object
//	})
//	desc, err := repo.GetComponentVersion(ctx, component, version)
//
// # Async Resolution
//
// GetComponentVersion is non-blocking. The first call enqueues work and returns
// [ErrResolutionInProgress]. Controllers should mark their status accordingly and return. There is
// no need to requeue the object because the workpool will enqueue reconcile events for the given
// objects using RequesterFunc function.
//
// # Cache Keys and Verification Separation
//
// Cache keys are an FNV-1a hash of: configHash | repoSpec | component | version | verifications | digest.
//
// Because verifications and digest are part of the key, a verified fetch and an unverified fetch
// for the same component version produce separate cache entries. This prevents an unverified
// cached result from being served to a requester that asked for verification.
//
// Use Verifications for top-level components where the Component CR specifies signatures.
// Use Digest for child components resolved through a reference path, where integrity is
// checked against the parent's reference digest rather than a standalone signature.
//
// # Error Handling
//
// Controllers must handle two sentinel errors from GetComponentVersion:
//
//   - [ErrResolutionInProgress]: resolution is queued and waiting to be done.
//   - [workerpool.ErrNotSafelyDigestible]: the component was resolved but cannot be digested.
//     The descriptor is still returned alongside the error.
//
// All other errors indicate resolution failure (network, verification mismatch, etc.).
//
// See ADR docs/adr/0009_controller_v2_lib_migration.md for architectural context.
package resolution
