# OCM v1 to v2 compatibility lane

Run `task compat/ocmv1:test` from the repository root. The task downloads the
checksum-verified OCM v1 CLI release pinned in
`bindings/go/cli/integration/.env`, builds the current CLI, then runs the
cross-version integration test.

CI runs the same test as a sibling of conformance and controller E2E after the
shared CLI build. It downloads that build's artifact and runs
`task compat/ocmv1:test/prebuilt OCM_V2_BIN=<path>` without rebuilding the CLI.

The test invokes OCM v1 to create and sign a component version in a temporary
CTF. It invokes the current CLI to read, verify, download, and transfer that
version from CTF and OCI sources. Generated artifacts, keys, registry data, and
transfer targets are temporary; the repository does not commit v1-generated
blob trees. Docker is required for the local OCI registry.
