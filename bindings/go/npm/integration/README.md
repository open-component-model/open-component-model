# npm integration tests

## OCM v1 producer → OCM v2 consumer

`Test_Integration_NPM_V1PackV2Access` runs the real **OCM v0.50.0** CLI to:

1. Pack a component constructor into a directory-format Common Transport Format (CTF) archive, retaining external npm access.
2. Generate the npm resource digest during packing.
3. Export the component descriptor as JSON.
4. Decode that descriptor with v2, download its npm resource, compare the tarball bytes and media type, and verify the **v1-generated digest** using v2.

The cases cover `npm`, `npm/v1`, `NPM`, and `NPM/v1`; unscoped and scoped packages; uppercase and unusual legacy names; `latest` and noncanonical version selectors; and a local `file://` registry. HTTP cases use a local test server, so no public npm packages or Docker are needed for this test.

This deliberately uses external `access`, not an npm `input`: the latter produces `localBlob`, which would test local-blob compatibility rather than the npm access implementation.

Download the appropriate executable from the [OCM v0.50.0 release](https://github.com/open-component-model/ocm/releases/tag/v0.50.0), verify it against the release checksums, and extract it outside the repository. Do not use the v2 `ocm` executable from this checkout.

From `bindings/go`, run:

```sh
OCM_V1_BINARY=/absolute/path/to/legacy/ocm \
  go test ./npm/integration -run '^Test_Integration_NPM_V1PackV2Access$' -count=1 -v
```

The test checks the executable version and skips explicitly when `OCM_V1_BINARY` is unset. It uses temporary archives and an isolated home directory. It does not download or install a binary automatically.

## Live npm registry

`Test_Integration_NPM` starts Verdaccio with testcontainers and tests anonymous, Basic and Bearer authentication, scoped packages, and digest processing. This test requires a working Docker runtime.

To run both tests from `bindings/go`:

```sh
OCM_V1_BINARY=/absolute/path/to/legacy/ocm go test ./npm/... -count=1
```
