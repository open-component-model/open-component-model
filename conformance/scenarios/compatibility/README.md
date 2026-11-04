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

The test invokes OCM v1 to create four component versions in temporary CTFs.
The signed version uses an uppercase prerelease, contains a local text resource
and references a second version, so the v1 signature covers a reference digest.
OCM v2 reads and verifies it, then lists and transfers the whole v1-produced TGZ
repository. It transfers that repository recursively, both by reference and with
the deprecated `--copy-resources`, and asserts for each run that the signatures
and the digest of every resource and reference are byte-equal to the source,
that the component is otherwise unchanged, and that the target still verifies.
It also downloads the resource bytes and checks the recorded digest. The third
version contains v1 Helm, OCI, Git, and S3 access specifications; OCM v2 reads
each one and checks its type and fields. OCM v1 creates that version with
digest generation disabled because its external resources are deliberately
not contacted. The fourth version uses local Helm and Git inputs; OCM v2 reads
their generated local-blob access and digest metadata, and downloads the Git
archive to check its recorded digest. Generated artifacts and keys are
temporary; the repository does not commit v1-generated blob trees.

This lane checks v1-produced data with v2, not cross-technology transfers.
The external access cases currently cover descriptor compatibility, not
download or digest verification of their remote content. A v1 OCI input case
still needs a local registry service; S3 has no v1 input type, so its case
checks the v1 S3 access record.

## Known gaps

Some cases pin the behaviour v2 shows today for v1 data that is known to be
mishandled. Each is tagged with `knownGap` and the number of its open issue
under [ocm-project#1366](https://github.com/open-component-model/ocm-project/issues/1366):
the case asserts the unfixed behaviour, so when a fix merges the case fails here
and is rewritten to assert the supported behaviour. Grep for `knownGap` for the
current list.

Two bugs under #1366 deliberately have no case here.
[#1405](https://github.com/open-component-model/ocm-project/issues/1405)
(`componentNameMapping: sha256-digest`) needs an OCI registry, which this
scenario does not provision.
[#1369](https://github.com/open-component-model/ocm-project/issues/1369)
(`localOciBlob`) is a descriptor access type no released v1 CLI writes any
more, so it is covered where its fix belongs, in `bindings/go/descriptor`.
