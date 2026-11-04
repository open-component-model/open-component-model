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

This lane checks v1-produced data with v2, not cross-technology transfers.
Generated artifacts and keys are temporary; the repository does not commit
v1-generated blob trees.

## What the lane covers

`ocmv1_signed_transfer_integration_test.go` has OCM v1 build a signed component
version with a local text resource and a reference to a second version, and
checks that v2 reads and verifies it. It lists and transfers the whole
v1-produced TGZ repository, and asserts after transfer — recursively, both by
reference and with the deprecated `--copy-resources` — that the signatures and
the digest of every resource and reference are byte-equal to the source, that
the whole component is unchanged, and that the target still verifies. It also
downloads the resource bytes and checks the recorded digest.

`ocmv1_access_integration_test.go` reads v1 Helm, OCI, Git, and S3 access
specifications and checks each one's type and fields. v1 creates that version
with digest generation disabled because its external resources are deliberately
not contacted. The external access cases cover descriptor compatibility, not
download or digest verification of their remote content. A v1 OCI input case
still needs a local registry service; S3 has no v1 input type, so its case
checks the v1 S3 access record.

`ocmv1_input_integration_test.go` uses local Helm and Git inputs, reads their
generated local-blob access and digest metadata, and downloads the Git archive
to check its recorded digest.

`ocmv1_realworld_integration_test.go` reads a component version shaped like the
transports users migrate: a large structured image-vector label on the
component and on a reference (Gardener), resource types spelled as media types,
legacy and versioned access spellings, and a Helm resource version carrying
semver build metadata (RBSC).

## Known gaps

`ocmv1_known_gaps_integration_test.go` pins the behaviour v2 shows today for v1
data that is known to be mishandled. Every case is linked to an open issue
under [ocm-project#1366](https://github.com/open-component-model/ocm-project/issues/1366)
through the `knownGap` helper. These cases are expected failures in the sense
that they assert the unfixed behaviour: when a fix merges, its case fails here
and is rewritten to assert the supported behaviour.

| Case | Issue |
| --- | --- |
| Certificate-chain signatures fail with `issuer mismatch` | [#1367](https://github.com/open-component-model/ocm-project/issues/1367) |
| Signed labels with `value: null` fail verification | [#1402](https://github.com/open-component-model/ocm-project/issues/1402) |
| `jsonNormalisation/v2` signatures cannot be verified | [#1403](https://github.com/open-component-model/ocm-project/issues/1403) |
| `--copy-resources` does not copy `s3/v1` resources | [#1404](https://github.com/open-component-model/ocm-project/issues/1404) |
| `--copy-resources` breaks the signature of v1 Helm resources | [#1368](https://github.com/open-component-model/ocm-project/issues/1368) |
| `localOciBlob` resources are dropped on transfer | [#1369](https://github.com/open-component-model/ocm-project/issues/1369) |
| Label `merge` is dropped on read and transfer | [#1371](https://github.com/open-component-model/ocm-project/issues/1371) |
| `ocm.software/v3alpha1` descriptors fail with a misleading error | [#1407](https://github.com/open-component-model/ocm-project/issues/1407) |

[#1405](https://github.com/open-component-model/ocm-project/issues/1405)
(`componentNameMapping: sha256-digest`) has no case: it needs an OCI registry,
which this scenario does not provision.

No released v1 CLI writes `localOciBlob` any more, so its case rewrites the
descriptor stored in a v1-produced CTF into the legacy shape v1 still reads.
`ocmv1_ctf_rewrite_test.go` holds that rewrite and recomputes the blob, config,
manifest, and index digests it invalidates.
