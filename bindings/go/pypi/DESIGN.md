# PyPI access type — design (`pypi/v1alpha1`)

This document explains the design of the `pypi` binding: the `pypi/v1alpha1`
access type and the resource repository, digest processor and transfer
transformer built on it. It is a self-contained binding under
`bindings/go/pypi`, orthogonal to the Maven binding (`maven/v2alpha1`): it
shares no code with Maven, imports no Maven package, and follows the same
binding / CLI / transfer wiring conventions so either can evolve independently.

## Goal

Let OCM reference a Python distribution (a project at a fixed version on a PyPI
index) as an external resource, and give it the same capability set the other
by-reference resource types have:

- **download** the distribution files (with their signatures) as one blob,
- **digest** that blob deterministically for signing/verification,
- **upload** the blob into a writable index (by-value transfer target),
- **transfer by value** through the transfer graph (`GetPyPIArtifact` → add
  local resource).

## Why PyPI is not a Maven variant

The two ecosystems resolve artifacts differently, so the binding is a parallel
implementation rather than a reuse of Maven's logic:

| Concern | Maven (`maven/v2alpha1`) | PyPI (`pypi/v1alpha1`) |
|---|---|---|
| Address | GAV coordinates + explicit `(extension, classifier)` file list | `indexUrl` + `project` + `version`; files **discovered** from the index |
| Resolution | file paths computed from coordinates; `maven-metadata.xml` for SNAPSHOT/LATEST/RELEASE | GET the project detail page, parse it, keep files whose parsed version matches |
| Serialization | XML metadata | **PEP 691 JSON** (preferred) or **PEP 503 HTML** (fallback), via `Accept` content negotiation |
| File selection | caller lists every file | `distributions` filter by kind (`sdist`/`wheel`) or exact filename; empty = all files of the version |
| Integrity siblings | `.asc` + `.md5/.sha1/.sha256/.sha512`, fetched blindly | index supplies per-file `hashes` inline; only `.asc` is a co-located sibling |
| Mutability | SNAPSHOT / LATEST / RELEASE are unpinned | a release version is **immutable** — always pinned |
| Credential identity | `MavenRepository` | `PyPIRepository` |

PyPI file names carry Python/ABI/platform tags (`requests-2.32.3-cp312-cp312-manylinux…​.whl`)
that cannot be reconstructed from `project` + `version`, so the file set must be
read from the index. That single fact drives the whole resolver.

## Access spec

`bindings/go/pypi/spec/access/v1alpha1`. Canonical wire type `pypi/v1alpha1`
(alias `PyPI/v1alpha1`). No prior OCM `pypi` access type exists, so `v1alpha1`
is the honest first version.

```go
type PyPI struct {
    Type          runtime.Type   // pypi/v1alpha1
    IndexURL      string         // Simple index base, e.g. https://pypi.org/simple
    Project       string         // distribution name, e.g. requests
    Version       string         // released version, e.g. 2.32.3
    Distributions []Distribution // empty => all files of the version
}

type Distribution struct {
    Kind     string // "sdist" | "wheel" | "" (any); ignored when Filename is set
    Filename string // exact filename; takes precedence over Kind
}
```

`Validate()` requires `indexUrl` (absolute URL), `project`, `version`, rejects
path separators / `..` in `project`, `version` and any explicit `filename`
(these become URL path segments), constrains `kind` to `sdist`/`wheel`, and
rejects duplicate distribution entries. `IsPinnedVersion()` is always `true`:
a PyPI release version names one fixed set of files, so there is no LATEST or
SNAPSHOT to reject as in Maven; the method exists so the digest processor and
transfer graph can apply the same pin check across access types.

`NormalizeProjectName` implements the PEP 503 normalization
(`re.sub(r"[-_.]+", "-", name).lower()`), so `Requests`, `foo.bar` and
`foo_bar` address the correct project detail URL.

## Resolution

`bindings/go/pypi/internal/pypi` holds the protocol.

1. Build the project detail URL: `<indexUrl>/<normalized-project>/` with the
   trailing slash the Simple API requires.
2. `GET` it with
   `Accept: application/vnd.pypi.simple.v1+json, application/vnd.pypi.simple.v1+html;q=0.2, text/html;q=0.01`.
3. If the response `Content-Type` is JSON → parse **PEP 691** (`files[]` with
   `filename`, `url`, `hashes`, `gpg-sig`, `yanked`). Otherwise parse **PEP 503**
   HTML (`<a href="…#sha256=…">filename</a>` with optional `data-gpg-sig` /
   `data-yanked`), using `golang.org/x/net/html`.
4. Resolve each file `url` against the detail URL (relative URLs are allowed).
5. Filter to `version` by parsing the version out of the filename (wheel: the
   second `-`-separated field; sdist: the tail of the `name-version` stem).
   Apply the `distributions` selector; skip yanked files unless one is named
   explicitly. Return in a deterministic order (sorted by filename) so the
   download archive is reproducible.

## Download blob shape

`bindings/go/pypi/repository/resource`. `DownloadResource` returns **one**
`application/x-tgz` for any file count. Entries are the selected distribution
files, each optionally followed by its `.asc` signature, stored exactly as
served (nothing is verified — the index's `hashes` and the signature are the
consumer's to check). The archive uses **stored (uncompressed) gzip blocks and
fixed tar headers** (no timestamps/owners), so its bytes depend only on the
files fetched, not on the Go release — this is what makes the digest stable.

This shape is deliberately identical to Maven's, so the digest processor and the
transfer wiring behave the same across both bindings.

## Digest

`bindings/go/pypi/digest` computes SHA-256 with the `genericBlobDigest/v1`
normalization over the download archive — the exact blob a by-value transfer
stores and later verifies. It fills an unset digest, verifies and canonicalizes
a pre-set one (accepting spelling variants), and rejects a mismatch or a
different algorithm. `EXCLUDE-FROM-SIGNATURE` short-circuits without
downloading. Because every PyPI version is immutable, every version is
digestable — there is no unpinned-version rejection.

## Upload

`UploadResource` is the inverse of download for a writable index
(Nexus/Artifactory PyPI-hosted repositories accept a file-addressed `PUT`). It
checks the archive (plain file names only, no duplicates, no signature without
its file) before the first `PUT`, then writes every entry unchanged to
`<indexUrl>/<normalized-project>/<filename>`. A failing `PUT` stops the upload
and leaves earlier entries deployed — an index has no transaction to roll back.

**Out of scope:** publishing to `upload.pypi.org`, which uses a `twine` / PEP 694
multipart `POST` to a separate endpoint rather than a file `PUT`. This mirrors
the Maven binding restricting itself to `PUT` and excluding SNAPSHOT deploy.

## Credentials

`bindings/go/pypi/spec/credentials/v1` defines `PyPICredentials/v1`
(`username`, `password`, `identityToken`). Resolution is keyed by the
`PyPIRepository` consumer identity built from `indexUrl`. An `identityToken`
yields `Bearer` auth; `username`/`password` yield Basic auth (an API token is
passed as the `__token__` username with the token as the password).
`DirectCredentials` property bags are accepted, including old OCM's
`accessToken` key. Nil credentials and a bag without any PyPI key mean an
anonymous request; a bag with only a password is an error rather than a silent
anonymous request.

## Transfer

`GetPyPIArtifact` (`bindings/go/pypi/transformation`) downloads the archive to a
local file so a later `AddLocalResource` embeds it as a local blob.
`processPyPI` (`bindings/go/transfer/internal`) emits the `Get` → `Add` node
pair. A PyPI resource has no OCI-artifact representation, so it always takes the
local-resource path regardless of the requested upload type — the same policy
wget and s3 use.

## CLI

`cli/internal/plugin/builtin/pypi` registers the resource repository, digest
processor and `PyPICredentials/v1` scheme, all sharing one HTTP client built
from the CLI's HTTP config, so timeouts, retries and TLS apply to every PyPI
request.

## Dependencies

The binding stays within the layered allow-set enforced by the `pypi` depguard
rule in `golangci.yml`: `blob`, `credentials`, `descriptor/runtime`,
`descriptor/v2`, `plugin`, `repository`, `runtime` (plus the third-party
`golang.org/x/net/html` for the PEP 503 fallback). It imports no other OCM
binding — in particular, nothing from `maven`.
