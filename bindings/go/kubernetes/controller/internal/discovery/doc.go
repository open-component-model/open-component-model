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
// The reference stage keeps only matching reference
// targets, the component stage only matching components, and the resource stage
// only components that have a matching resource.
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
// A missing access (missing map key, missing attribute, out-of-range index) is
// not an error: it is a selector nonmatch and an omitted extraction field. All
// three shapes come from one unexported cel-go error type. See `missingAccessPrefixes`.
//
// Three near-misses are terminal instead:
//
//   - A present-but-null field. descriptor/v2 has no omitempty, so
//     size(component.resources) on a component without resources is
//     "no such overload". Guard with 'x == null ? 0 : size(x)'.
//   - A missing access in extract.expression, strict by design, unlike the
//     byResources and byComponents field maps.
//   - An expression returning null or optional.none(), which stores an explicit
//     null rather than omitting the field.
//
// All other CEL errors are SelectorError or ExtractError, which the controller
// reports as a configuration failure. Descriptor conversion, marshalling, or
// decoding failures surface from Project as ordinary wrapped errors with the
// component name/version. A resource removed by selection is not serialized, so
// a bad access on a discarded resource will never fail.
//
// An empty selector stage is not a failure: Filter reports which stage emptied
// the result and Project deterministically emits an empty list. Descriptor
// inputs are never mutated.
package discovery
