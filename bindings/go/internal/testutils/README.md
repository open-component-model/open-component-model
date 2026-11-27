# testutils

Reusable test fixtures shared across the `bindings/go` module.

## Test-only policy

Production code must never import anything under `testutils`. Several independent layers enforce this:

- The package lives under `bindings/go/internal/`, so no module outside `bindings/go` can import it.
- The `testutils-test-only` depguard rule in `golangci.yml` fails lint for any non-test file (`*.go` that is not `*_test.go`) that imports a `testutils` package. Files inside the `testutils` tree are exempt, so subpackages can share helpers.
- The `internal-testutils` depguard rule in `golangci.yml` limits what the `testutils` tree itself may import to the standard library and go-git, so a fixture cannot pull a heavy binding (for example `cli`) into the test-only tree.
- The production dependency-closure test in `production_closure_test.go` runs under `task bindings/go:test` and fails if `testutils` is reachable from the non-test build graph of any package in the module. It shells out to `go list` and does not depend on golangci, so it catches a leak even if the lint rule is removed or skipped. It checks each build configuration in its table (`linux/amd64`, `linux/amd64` with the `pprof` tag, `darwin/arm64` and `windows/amd64`), so it also sees a leak behind an OS-specific file or a custom build tag. The test runs on every PR because the linux unit-test job runs it.

Go does not compile a package's `_test.go` files for importers, so a helper reached only through `_test.go` files never enters a downstream consumer's production build. The layers above turn that property into a hard guarantee and keep heavy fixtures (git servers, synthetic descriptors, registries) out of the dependency graph that consumers of the library compile.

## Adding a consumer

Each binding's depguard allow list denies sibling `bindings/go/...` imports that are not listed. When a binding's tests start importing `testutils`, add `ocm.software/open-component-model/bindings/go/internal/testutils` to that binding's allow list in `golangci.yml`. The `testutils-test-only` rule still blocks non-test files in that binding.

## Packages

- `git` builds throwaway Git repositories for tests (`Repository`).
