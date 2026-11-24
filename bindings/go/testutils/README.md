# testutils

Reusable test fixtures shared across the `bindings/go` module.

## Test-only policy

Production code must never import anything under `testutils`. The rule is enforced by the `testutils-test-only` depguard rule in `golangci.yml`: any non-test file (`*.go` that is not `*_test.go`) that imports a `testutils` package fails lint. Files inside the `testutils` tree are exempt, so subpackages can share helpers.

Go does not compile a package's `_test.go` files for importers, so a helper reached only through `_test.go` files never enters a downstream consumer's production build. The depguard rule turns that property into a hard guarantee and keeps heavy fixtures (git servers, synthetic descriptors, registries) out of the dependency graph that consumers of the library compile.

## Adding a consumer

Each binding's depguard allow list denies sibling `bindings/go/...` imports that are not listed. When a binding's tests start importing `testutils`, add `ocm.software/open-component-model/bindings/go/testutils` to that binding's allow list in `golangci.yml`. The `testutils-test-only` rule still blocks non-test files in that binding.

## Packages

- `git` builds throwaway Git repositories for tests (`Repository`).
