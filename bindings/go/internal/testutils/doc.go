// Package testutils and its subpackages hold reusable test fixtures shared
// across the bindings/go module.
//
// It lives under internal/ so modules outside bindings/go cannot import it, and
// it is a test-only helper library: production code inside bindings/go must
// never import anything under testutils either. That policy has three
// independent layers of enforcement:
//
//   - The internal/ placement keeps it off the importable surface of any other
//     module.
//   - The "testutils-test-only" depguard rule in golangci.yml fails the lint for
//     any non-test file that imports a testutils package.
//   - A production dependency-closure test (production_closure_test.go) fails if
//     testutils is reachable from the non-test build graph of any package in the
//     module, independent of golangci.
//
// Keeping testutils out of the production import graph stops its heavy fixtures
// (git servers, registries, synthetic descriptors) from leaking into the
// dependency set that consumers of the library compile.
package testutils
