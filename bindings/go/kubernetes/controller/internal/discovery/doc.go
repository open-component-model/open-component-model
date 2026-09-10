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
// Bindings exposed to CEL expressions depend on the stage:
//   - selectors: identity (map of string to string), labels (label name to decoded JSON value)
//   - extract.byResources: component (inner component), resource
//   - extract.byComponents: component (inner component)
//   - extract.expression: components (list of full v2 descriptors)
//
// Missing field or key access is not an error: it is a selector nonmatch and an
// omitted extraction field. All other CEL errors (type errors, nonboolean selector
// results, invalid SemVer inputs, cancellation) are reported as structured
// SelectorError or ExtractError values.
//
// Empty reference or component selector stages are not failures: Filter reports
// them with a distinct EmptyReason and Project deterministically emits an empty list.
// Descriptor inputs are never mutated.
package discovery
