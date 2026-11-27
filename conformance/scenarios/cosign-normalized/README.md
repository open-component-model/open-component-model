# Cosign Normalized Layout Scenario

Checks that a component version published in the normalized OCI layout
([ADR 0034](../../../docs/adr/0034_normalized_oci_layout.md)) can be signed with
plain `cosign` like an image, and that the signature stays valid through
`ocm sign` and `ocm transfer` without re-signing. It also shows the payoff: a
consumer with only `cosign` and plain registry HTTP (no `ocm`) can authenticate
and read the component. `ocm` and `cosign` both come from the full OCM CLI image.

## Run

```bash
task run                                  # uses ghcr.io/open-component-model/cli:main
task run CLI_IMAGE=ocm-cli:dev            # a locally built image
```

Requires Docker, `curl`, `jq` and `openssl`; no cluster is involved. Two
[zot](https://zotregistry.dev) registries (OCI referrers API) run on a private
Docker network and are published on `127.0.0.1:15001` and `127.0.0.1:15002`
(`SOURCE_PORT` / `TARGET_PORT` to override).

## Steps

| Task | Expectation |
| --- | --- |
| `config` | Writes `tmp/scenario.ocmconfig` with an `oci.storage.config.ocm.software/v1alpha1` entry (`componentVersionLayout: normalized`) and an `http.config.ocm.software/v1alpha1` entry (`plainHTTP: true`), passed to every `ocm` command below via `--config`. |
| `publish` | `ocm add cv` publishes `acme.org/cosign/normalized:1.0.0` with a local blob; the tag resolves to the `component-descriptor.normalized.v1` manifest. |
| `cosign:sign` | `cosign sign` on the manifest digest succeeds and `cosign verify` passes on the source registry. |
| `ocm:sign` | `ocm sign cv` adds an RSA signature and rewrites the version in the configured normalized layout; the tag still resolves to the same manifest digest, and both the cosign and the OCM signature verify. |
| `transfer` | Source → CTF → target with the normalized layout configured: the signature referrer is carried, and the target tag resolves to the same manifest digest. |
| `verify:target` | `cosign verify` and `ocm verify cv` pass on the target; `ocm download resource` returns the original content. |
| `verify:cosign-only` | Using only `cosign` and plain registry HTTP (no `ocm`): `cosign verify` the signature over `D_norm`, read the access-free descriptor from the signed manifest, and bind each resource's content to the signed digest. |

Keys, configuration and the CTF live in `tmp/`; `task clean` removes them,
the registries and the network.

## Host-side walkthrough (`demo.sh`)

`demo.sh` is the narrative companion to the Taskfile above: a single, self-contained
script that tells the same story interactively, for live demos and as the basis of the
blog post. It differs from the Taskfile in three ways:

- it uses a local `ocm` binary (the CLI built from this checkout, `task bindings/go/cli:build`)
  and a downloaded `cosign`, instead of the full CLI image;
- it carries the component across an airgap as a CTF (`oci -> ctf -> oci`) and relocates an
  external **OCIImage** resource (`podinfo`) into the transport, so you can watch the access
  descriptor get rewritten while the signed digest `D_norm` stays identical;
- it prints each command and waits for Enter (`PAUSE=0` runs straight through), showing the
  CLI's live progress display.

```bash
conformance/scenarios/cosign-normalized/demo.sh          # Enter-stepped
PAUSE=0 conformance/scenarios/cosign-normalized/demo.sh   # straight through
```

Requires Docker, `oras`, `curl`, `jq`, `openssl`; `cosign` is used from `PATH` or downloaded
and cached under `$TMPDIR`. It starts its own two zot registries, removes them on exit, and
prints a temporary work directory holding the keys, configs and CTF. `OCM`, `COSIGN`,
`SOURCE_PORT`, `TARGET_PORT` and `PODINFO` are overridable via the environment. It is not run
by CI; the Taskfile scenario is the conformance check.

## Three-minute demo (`demo-short.sh`)

`demo-short.sh` steps through exactly the commands of the "Sign, Transfer, Verify" section of the
[blog post](../../../website/content/blog/2026-11-26-cosign-signable-components.md): publish a component
version with an image reference, `cosign sign` it, `ocm transfer` it with the image to a second registry,
and `cosign verify` it there. Shell wrappers add what only the local setup needs (plain HTTP, no
transparency log), so the commands on screen are those of the post.

```bash
conformance/scenarios/cosign-normalized/demo-short.sh          # Enter-stepped
PAUSE=0 conformance/scenarios/cosign-normalized/demo-short.sh   # straight through
```

Requires Docker, `oras`, `cosign`, `yq` and the CLI built from this checkout. The registries run on
`source.localhost:5001` and `target.localhost:5002`. The `podinfo` image is cached under `$TMPDIR` after
the first run, so later runs need no internet.
