# OCM conformance scenario: Compatibility

Run `task conformance/compatibility:run` from the repository root. The task downloads the
checksum-verified OCM v1 CLI release pinned in
`conformance/scenarios/compatibility/.env`, builds the current CLI, then runs the
cross-version conformance test. You can also run `task run` from this directory.
The tests in `tests/conformance/` reuse the dependencies pinned by
`bindings/go/go.mod`; no separate Go module is needed.
Prerequisites: Go, Task, Git, OpenSSL, curl, and tar.

CI runs this scenario alongside the sovereign scenario in the conformance
workflow after the shared builds. It downloads that build's artifact and runs
`task conformance/compatibility:test OCM_V2_BIN=<path>` without rebuilding the CLI.

The test invokes OCM v1 to create three component versions in temporary CTFs.
The signed version contains a local text resource; OCM v2 reads it, verifies
the signature, downloads its bytes, and checks its recorded digest. The second
version contains v1 Helm, OCI, Git, and S3 access specifications; OCM v2 reads
each one and checks its type and fields. OCM v1 creates that version with
digest generation disabled because its external resources are deliberately
not contacted. The third version uses local Helm and Git inputs; OCM v2 reads
their generated local-blob access and digest metadata, and downloads the Git
archive to check its recorded digest. Generated artifacts and keys are
temporary; the repository does not commit v1-generated blob trees.

This lane checks v1-produced data with v2, not cross-technology transfers.
The external access cases currently cover descriptor compatibility, not
download or digest verification of their remote content. A v1 OCI input case
still needs a local registry service; S3 has no v1 input type, so its case
checks the v1 S3 access record.
