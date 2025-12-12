// Package resolution contains details for the component resolution service. The following actions are taken
// once a resolution request is received through the reconciler.
//
//	Input: ResolveComponentVersion(opts)
//	 	↓
//	Resolver.ResolveComponentVersion()
//	  	├─ Validate options
//	  	├─ Load configurations
//	  	└─ Build cache key
//		  ↓
//	WorkerPool.Resolve(key, opts, cfg)
//	  	├─ Check cache (fast path) → return if hit
//	  	├─ Check inProgress map → return ErrResolutionInProgress if exists
//	  	├─ Add to inProgress + increment gauge
//	  	└─ Enqueue to work channel → return ErrResolutionInProgress
//		  ↓
//	WorkerPool.worker (one of N concurrent workers)
//		├─ Read from work queue
//		├─ Call resolve(opts, cfg)
//		├─ Write result directly to cache
//		└─ Remove from inProgress + decrement gauge
//
// Once the resolution request is done, we return the repository spec ( for further processing like downloading ) the
// discovered and processed descriptor and the config hash. The cache key is generated with the following algorithm:
//
//	Compute key = SHA-256(Canonical(config) + Canonical(repoSpec) + componentName + version)
//
// Canonical in this context means that the same hash is generated for the same config/repoSpec ALWAYS.
// Once the cache key has been created and a result is stored the cache has a TTL of ( default ) 30 minutes. Once the
// TTL hits, results are cleared by a separate go routine.
// The process and the service are described by ADR docs/adr/0009_controller_v2_lib_migration.md.
package resolution
