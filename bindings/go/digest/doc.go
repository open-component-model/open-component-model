// Package digest holds the shared rules for generating and verifying the resource digest
// recorded in a component descriptor.
//
// The vocabulary itself is not restated here: the normalisation algorithms and the
// exclusion marker are the constants of [descriptor], and the hash names a digest may
// carry resolve in [algorithms.Hash].
//
// The package knows nothing about where a digest comes from. A binding computes the
// digest of the content it resolved or hashed and supplies the [Rule] for that kind of
// content; [Check] then decides, the same way for every binding, what happens to the
// digest recorded in the component descriptor.
package digest
