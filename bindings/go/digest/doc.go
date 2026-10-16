// Package digest holds the resource digest vocabulary of the OCM specification and the
// shared rules for generating and verifying a recorded resource digest.
//
// The package knows nothing about where a digest comes from. A binding computes the
// digest of the content it resolved or hashed and supplies the [Rule] for that kind of
// content; [Check] then decides, the same way for every binding, what happens to the
// digest recorded in the component descriptor.
package digest
