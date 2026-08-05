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
| 3 | `gpg`, `rsa` | `credentials`, `descriptor/runtime`, `runtime` |
| 3 | `repository` | `blob`, `configuration`, `credentials`, `descriptor/runtime`, `runtime` |
| 3 | `transform` | `cel`, `credentials`, `dag`, `runtime` |
| 4 | `oci` | `blob`, `configuration`, `credentials`, `ctf`, `descriptor/runtime`, `descriptor/v2`, `http`, `repository`, `runtime` |
| 4 | `signing` | `configuration`, `descriptor/normalisation`, `descriptor/runtime`, `runtime` |
| 5 | `constructor` | `blob`, `credentials`, `ctf`, `dag`, `descriptor/normalisation`, `descriptor/runtime`, `descriptor/v2`, `oci`, `repository`, `runtime` |
| 5 | `sigstore` | `credentials`, `descriptor/runtime`, `runtime`, `signing` |
| 6 | `input/dir`, `input/file`, `input/utf8` | `blob`, `constructor`, `runtime` |
| 6 | `plugin` | `blob`, `configuration`, `constructor`, `credentials`, `descriptor/runtime`, `descriptor/v2`, `repository`, `runtime`, `signing` |
| 6 | `wget` | `blob`, `configuration`, `constructor`, `credentials`, `descriptor/runtime`, `descriptor/v2`, `http`, `repository`, `runtime` |
| 7 | `github` | `blob`, `credentials`, `descriptor/runtime`, `descriptor/v2`, `http`, `plugin`, `repository`, `runtime` |
| 7 | `helm` | `blob`, `configuration`, `constructor`, `credentials`, `descriptor/runtime`, `descriptor/v2`, `http`, `oci`, `plugin`, `repository`, `runtime` |
| 8 | `transfer` | `blob`, `configuration`, `credentials`, `ctf`, `dag`, `descriptor/runtime`, `descriptor/v2`, `github`, `helm`, `oci`, `repository`, `runtime`, `signing`, `transform`, `wget` |
