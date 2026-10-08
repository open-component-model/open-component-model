# FIPS GPG Scenario

Checks that the full OCM CLI image (`ghcr.io/open-component-model/cli:<version>`)
signs and verifies component versions with GPG while running under
`GODEBUG=fips140=only`, the strictest FIPS 140-3 mode of OCM. In this mode OCM
only runs a `gpg` whose `libgcrypt` is in FIPS mode, so the scenario covers the
image's Garden Linux GnuPG as well as OCM's FIPS gate.

## Run

```bash
task run                                  # uses ghcr.io/open-component-model/cli:main
task run CLI_IMAGE=ocm-cli:dev            # a locally built image
```

Only Docker is required; no cluster is involved.

## Steps

| Task | Expectation |
| --- | --- |
| `keys` | The image's own `gpg` (FIPS-mode `libgcrypt`) generates an RSA-3072 signing key and an unrelated key. |
| `build` | `ocm add cv` creates `acme.org/fips/gpg:1.0.0` with a local file resource in a CTF. |
| `sign:libgcrypt-not-fips` | With `/etc/gcrypt/fips_enabled` hidden, `libgcrypt` leaves FIPS mode and `ocm sign cv` must fail with `require a gpg whose libgcrypt runs in FIPS mode`. |
| `sign` | `ocm sign cv` with the GPG signer succeeds. |
| `verify` | `ocm verify cv` with the signer's public key succeeds. |
| `verify:wrong-key` | `ocm verify cv` with the unrelated public key must fail with `SIGNATURE VERIFICATION FAILED`. |

All `ocm` invocations run with `GODEBUG=fips140=only`. Generated keys,
configuration and the CTF live in `tmp/`; `task clean` removes them and the
GnuPG Docker volume.
