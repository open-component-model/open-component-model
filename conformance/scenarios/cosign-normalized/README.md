# Cosign Normalized Layout Scenario

Checks that a component version published in the normalized OCI layout
([ADR 0032](../../../docs/adr/0032_normalized_oci_layout.md)) can be signed with
plain `cosign` like an image, and that the signature stays valid through
`ocm sign` and `ocm transfer` without re-signing. `ocm` and `cosign` both come
from the full OCM CLI image.

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
| `publish` | `ocm add cv --layout normalized` publishes `acme.org/cosign/normalized:1.0.0` with a local blob; the tag resolves to the `component-descriptor.normalized.v1` manifest. |
| `cosign:sign` | `cosign sign` on the manifest digest succeeds and `cosign verify` passes on the source registry. |
| `ocm:sign` | `ocm sign cv` adds an RSA signature; the tag still resolves to the same manifest digest, and both the cosign and the OCM signature verify. |
| `transfer` | Source → CTF → target without `--layout`: the layout is inherited, the signature referrer is carried, and the target tag resolves to the same manifest digest. |
| `verify:target` | `cosign verify` and `ocm verify cv` pass on the target; `ocm download resource` returns the original content. |

Keys, configuration and the CTF live in `tmp/`; `task clean` removes them,
the registries and the network.
