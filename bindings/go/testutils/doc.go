// Package testutils and its subpackages hold reusable test fixtures shared
// across the bindings/go module.
//
// It is a test-only helper library. Production code must never import anything
// under testutils. The rule is enforced by depguard in golangci.yml (see the
// "testutils-test-only" rule): any non-test file that imports a testutils
// package fails the lint. Keeping testutils out of the production import graph
// stops its heavy fixtures (git servers, registries, synthetic descriptors)
// from leaking into the dependency set that downstream consumers of the library
// compile.
package testutils
