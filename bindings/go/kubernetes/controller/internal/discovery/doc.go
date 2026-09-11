// Package discovery implements the Kubernetes-independent evaluation engine for
// Discovery resources: it resolves the complete transitive component graph via
// a configured repository resolver (see Traverse), compiles the selector and
// extraction configuration of a DiscoverySpec once, filters the resolved graph,
// and projects the filtered descriptors into raw v2 descriptor JSON or
// free-form extracted records.
//
// Traversal is not restricted to a single repository: each component identity
// is routed to a repository by the resolver following the configured resolver
// precedence (path matchers or deprecated fallback resolvers, with an optional
// high-priority root pattern and root-repository catch-all).
//
// The evaluation pipeline per reconcile is:
//
//	Compile(spec) -> Query
//	Query.Filter(ctx, graph) -> Filtered   (reference -> component -> resource stages)
//	Query.Project(ctx, filtered) -> Payload (raw | byResources | byComponents | expression)
//
// Filtering keeps runtime descriptors: it performs no v2 conversion or
// serialization, only label decoding for selector evaluation. Filtered is a
// read-only view over the graph, not an isolated snapshot: Filter owns the
// result slice and each surviving descriptor's resource slice, but shares all
// other nested data read-only with the input. Neither the graph nor its
// descriptors are mutated. v2 JSON and generic CEL maps are materialized only
// in Project, and only for the selected output.
//
// Bindings exposed to CEL expressions depend on the stage:
//   - selectors: identity (map of string to string), labels (label name to decoded JSON value)
//   - extract.byResources: component (inner component), resource
//   - extract.byComponents: component (inner component)
//   - extract.expression: components (list of full v2 descriptors)
//
// Missing field or key access is not an error: it is a selector nonmatch and an
// omitted extraction field. All other CEL errors (type errors, nonboolean selector
// results, invalid SemVer inputs, cancellation) are reported as structured
// SelectorError or ExtractError values. Descriptor conversion, marshalling, or
// decoding failures surface from Project as ordinary wrapped errors carrying the
// component name/version, not as ExtractError, so the controller treats them as
// retryable rather than terminal. A resource removed by selection is never
// serialized, so a bad access on a discarded resource cannot fail projection.
//
// Empty reference or component selector stages are not failures: Filter reports
// them with a distinct EmptyReason and Project deterministically emits an empty list.
// Descriptor inputs are never mutated.
package discovery
