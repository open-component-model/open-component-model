# Binding Dependency Layers

| Layer | Module | Direct dependencies |
|-------|--------|---------------------|
| 0 | `cel`, `dag`, `runtime` | — |
| 1 | `blob`, `configuration`, `descriptor/v2`, `generator` | `runtime` |
| 2 | `credentials` | `configuration`, `dag`, `runtime` |
| 2 | `ctf` | `blob` |
| 2 | `descriptor/runtime` | `descriptor/v2`, `runtime` |
| 2 | `http` | `configuration`, `runtime` |
| 3 | `descriptor/normalisation` | `descriptor/runtime`, `descriptor/v2`, `runtime` |
| 3 | `gpg` | `credentials`, `descriptor/runtime`, `runtime` |
| 3 | `transform` | `cel`, `credentials`, `dag`, `runtime` |
| 4 | `signing` | `configuration`, `credentials`, `descriptor/normalisation`, `descriptor/runtime`, `runtime` |
| 5 | `repository` | `blob`, `configuration`, `credentials`, `descriptor/normalisation`, `descriptor/runtime`, `runtime`, `signing` |
| 5 | `rsa`, `sigstore` | `credentials`, `descriptor/runtime`, `runtime`, `signing` |
| 6 | `oci` | `blob`, `configuration`, `credentials`, `ctf`, `descriptor/runtime`, `descriptor/v2`, `http`, `repository`, `runtime` |
| 7 | `constructor` | `blob`, `credentials`, `ctf`, `dag`, `descriptor/normalisation`, `descriptor/runtime`, `descriptor/v2`, `oci`, `repository`, `runtime` |
| 8 | `git`, `s3`, `wget` | `blob`, `configuration`, `constructor`, `credentials`, `descriptor/runtime`, `descriptor/v2`, `http`, `repository`, `runtime` |
| 8 | `input/dir`, `input/file`, `input/utf8` | `blob`, `constructor`, `runtime` |
| 8 | `plugin` | `blob`, `configuration`, `constructor`, `credentials`, `descriptor/runtime`, `descriptor/v2`, `repository`, `runtime`, `signing` |
| 9 | `github` | `blob`, `credentials`, `descriptor/runtime`, `descriptor/v2`, `http`, `plugin`, `repository`, `runtime` |
| 9 | `helm` | `blob`, `configuration`, `constructor`, `credentials`, `descriptor/runtime`, `descriptor/v2`, `http`, `oci`, `plugin`, `repository`, `runtime` |
| 10 | `transfer` | `blob`, `cel`, `configuration`, `credentials`, `ctf`, `dag`, `descriptor/normalisation`, `descriptor/runtime`, `descriptor/v2`, `git`, `github`, `helm`, `http`, `oci`, `repository`, `runtime`, `s3`, `signing`, `transform`, `wget` |
| 11 | `cli` | `blob`, `cel`, `configuration`, `constructor`, `credentials`, `ctf`, `dag`, `descriptor/normalisation`, `descriptor/runtime`, `descriptor/v2`, `git`, `github`, `gpg`, `helm`, `http`, `input/dir`, `input/file`, `input/utf8`, `oci`, `plugin`, `repository`, `rsa`, `runtime`, `s3`, `signing`, `sigstore`, `transfer`, `transform`, `wget` |
| 11 | `kubernetes/controller` | `blob`, `cel`, `configuration`, `constructor`, `credentials`, `ctf`, `dag`, `descriptor/normalisation`, `descriptor/runtime`, `descriptor/v2`, `git`, `github`, `gpg`, `helm`, `http`, `oci`, `plugin`, `repository`, `rsa`, `runtime`, `s3`, `signing`, `sigstore`, `transfer`, `transform`, `wget` |
