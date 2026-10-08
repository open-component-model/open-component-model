// Package resolution provides component version fetching with optional signature verification
// and digest integrity checking.
//
// # Usage
//
// Create a [Resolver] during controller setup using [NewResolver].
//
//	resolver := resolution.NewResolver(logger)
//
// To fetch a component version, call [Resolver.GetComponentVersion] with [Options] and a [Verification]:
//
//	cfg, err := configuration.LoadConfigurations(ctx, client, namespace, ocmConfigs)
//	pm, err := setup.NewPluginManager(ctx, cfg.Config, logger)
//	opts := &resolution.Options{
//	    RepositorySpec: repoSpec,
//	    Configuration:  cfg,
//	    PluginManager:  pm, // required; built from cfg
//	}
//	desc, err := resolver.GetComponentVersion(ctx, opts,
//	    resolution.Verification{Verifications: verifications}, component, version)
//
// GetComponentVersion blocks until the component version is fetched from the repository and verified.
// Nothing is cached between calls, so every call reaches the repository.
//
// Everything that needs no verification (listing versions, local blobs, health checks, transfers) uses the
// repository resolver from [Resolver.RepositoryResolver] directly.
//
// # Request-Scoped Plugins
//
// Plugins are configured from the reconciled object's ocmConfig, so the caller builds a
// PluginManager per reconcile and passes it in. The resolver caches resolvers under the
// configuration hash for 30 minutes. That works, because the manager is itself derived from the
// same configuration.
//
// # Verification
//
// Set [Verification.Verifications] for top-level components, built from the signing.config.ocm.software
// entries of the reconciled object's OCM configuration (see [verification.GetVerifications]).
// Set [Verification.Digest] for child components resolved through a reference path, where integrity is
// checked against the parent's reference digest rather than a standalone signature.
// The two are mutually exclusive.
//
// # Error Handling
//
// Controllers must handle one sentinel error from GetComponentVersion:
//
//   - [ErrNotSafelyDigestible]: the component was resolved but cannot be digested.
//     The descriptor is still returned alongside the error.
//
// All other errors indicate resolution failure (network, verification mismatch, etc.).
package resolution
