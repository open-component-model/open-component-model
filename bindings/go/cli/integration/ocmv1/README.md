# OCM v1 to v2 compatibility lane

Run `task compat/ocmv1:test` from the repository root. The task downloads the
checksum-verified OCM v1 CLI release pinned in
`bindings/go/cli/integration/.env`, builds the current CLI, then runs the
cross-version integration test.

CI runs the same test as a sibling of conformance and controller E2E after the
shared CLI build. It downloads that build's artifact and runs
`task compat/ocmv1:test/prebuilt OCM_V2_BIN=<path>` without rebuilding the CLI.

The test invokes OCM v1 to create three component versions in temporary CTFs.
The signed version contains a local text resource; OCM v2 reads it, verifies
the signature, downloads its bytes, and checks its recorded digest. The other
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
