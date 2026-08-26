# Optional `component-descriptors` Path Prefix for Component Versions

* **Status**: proposed
* **Deciders**: OCM Technical Steering Committee
* **Date**: 2026-05-27

**Technical Story**:

The fixed `component-descriptors` segment in every OCI repository path is a namespace
convention from the first OCM spec. As OCM adoption grows and users embed component
versions inside existing OCI registries, this extra path segment creates friction:
it is surprising, hard to explain, and prevents storing a component version at the
"natural" location a team would choose. This ADR proposes making the segment optional
while preserving full backward compatibility.

---

## Context and Problem Statement

Every OCM component version stored in an OCI registry today lives at a path of the form:

```text
<registry>[/<subPath>]/component-descriptors/<component-name>:<version>
```

Example:

```text
ghcr.io/my-org/platform/component-descriptors/ocm.software/myapp:v1.2.0
```

The `component-descriptors` segment was introduced as a namespace guardrail: it
separates OCM component data from arbitrary OCI artifacts that might share the same
registry prefix.

### Why this is a problem today

1. **OCI path ergonomics.** Users expect to address a component at its logical location.
   Teams that store their container images at `ghcr.io/my-org/platform/myapp` want their
   component version at `ghcr.io/my-org/platform/myapp`, not at
   `ghcr.io/my-org/platform/component-descriptors/myapp`. The extra segment is confusing
   and non-discoverable.

2. **Redundancy when `subPath` already namespaces.** A registry tenant that already owns
   a dedicated sub-path (e.g. `registry.example.com/ocm/`) gains nothing from
   `component-descriptors/` — it duplicates a namespace that already exists.

3. **Friction for native OCI tooling.** Tools that speak plain OCI (skopeo, crane, ORAS)
   have to be told about this extra segment. There is no way for an OCI client to discover
   it from a registry listing without knowing the OCM convention.

4. **Growing inconsistency with the rest of the OCM spec.** The component name itself is
   already a globally unique, domain-scoped identifier (`<domain>/<path…>`). It provides
   sufficient namespace isolation on its own.

### Current parser state

The Go bindings parser (`compref.Parse`) already treats the prefix as one of two valid
options: `"component-descriptors"` or `""` (empty). The double-slash notation
`ghcr.io/my-org//ocm.software/myapp:v1.2.0` already round-trips through the parser
today. The restriction is on the **write path**: both the OCI resolver and the CTF store
hard-code `component-descriptors` when constructing the OCI repository reference used
for push/pull. The CTF lister also hard-filters on the prefix, making components written
without it invisible.

---

## Decision Drivers

* Backward compatibility is non-negotiable: every existing component version at a
  `component-descriptors/…` path must continue to be readable and transferable without
  any migration step.
* The OCM spec (`doc/04-extensions/03-storage-backends/oci.md`) currently mandates the
  prefix. Any code change here requires a coordinated spec PR.
* The change must not silently break CTF archives created with existing tooling.
* The no-prefix form should be opt-in for now, to allow gradual ecosystem rollout.

---

## Considered Options

* **Option A** — Make the prefix configurable and opt-in (no default change)
* **Option B** — Deprecate the prefix and flip the default to empty in a future major release
* **Option C** — Keep the prefix mandatory; address ergonomics through aliases or registry-side mapping

---

## Decision Outcome

Chosen [Option A](#option-a-configurable-opt-in-prefix).

The prefix stays mandatory by default. Callers can opt out by passing a
`WithComponentDescriptorPath("")` option when constructing a resolver or CTF store.
The CTF lister is made prefix-agnostic so it can read archives written with either
convention. This is a stepping stone: once the spec has been updated and ecosystem
tooling has adapted, a later ADR can flip the default (Option B).

### Open angles requiring community discussion

The following questions are deliberately left open in this ADR:

1. **`Ref.String()` serialisation for empty prefix.** When `Prefix == ""`, the current
   serialiser emits `//` (double-slash). This is unambiguous and already parsed
   correctly, but aesthetically awkward. The alternative is to emit a single `/` and
   teach the parser to treat a component name appearing immediately after the repository
   path (without any known prefix) as the no-prefix case. The double-slash form is
   proposed as the interim canonical representation, pending a UX review.

2. **Spec relaxation scope.** The spec PR should decide whether `component-descriptors`
   becomes a `SHOULD` (recommended but not required), a named configuration option, or
   is deprecated with a sunset timeline. This ADR does not prescribe which.

3. **Migration tooling.** Should the `ocm transfer` command grow a flag to rewrite
   component references to the no-prefix form during transfer? Out of scope here but
   worth a follow-up issue.

4. **CTF compatibility fixtures.** The existing test fixtures under
   `bindings/go/ctf/testdata/compatibility/` embed `component-descriptors` in their
   `artifact-index.json` files. New fixtures for the no-prefix form should be added
   alongside (not replacing) the existing ones.

---

### Option A: Configurable, opt-in prefix

#### Description

Add a `componentDescriptorPath string` field to the two write-side types that currently
hard-code the prefix. The field defaults to `"component-descriptors"`. A new option
`WithComponentDescriptorPath(path string)` lets callers override it, including passing
`""` to omit the segment entirely.

The CTF component lister is updated to accept entries stored under any of the valid
prefixes (including none), so a single archive can mix conventions and list correctly.

No changes are required to `compref.Parse`, `validate.ComponentVersionDescriptor`,
or `annotations.ParseComponentVersionAnnotation` — all three already handle the prefix
defensively.

#### Impacted code paths

| Location | Nature of change |
|----------|-----------------|
| `bindings/go/oci/resolver/url/resolver.go` | Add `componentDescriptorPath` field; `BasePath()` uses it instead of constant |
| `bindings/go/oci/resolver/url/options.go` | Add `WithComponentDescriptorPath` option |
| `bindings/go/oci/ctf/store.go` | Add `prefix` field; `ComponentVersionReference` uses it |
| `bindings/go/oci/ctf/lister.go` | Iterate all `compref.ValidPrefixes` when scanning index |
| `bindings/go/oci/compref/compref_test.go` | Add no-prefix round-trip cases |
| `bindings/go/oci/resolver/url/resolver_test.go` | Test `WithComponentDescriptorPath("")` |
| `bindings/go/oci/ctf/lister_test.go` | Test listing entries written without prefix |

No changes to:

- `bindings/go/oci/spec/repository/path/path.go` (constant stays for compat)
- `bindings/go/oci/compref/compref.go` (parser already handles both)
- `bindings/go/oci/internal/validate/validate.go` (already strips defensively)
- `bindings/go/oci/spec/annotations/annotations.go` (already strips defensively)

#### Contract

Write-side (resolver):

```go
// Existing — unchanged behaviour:
resolver, _ := url.New(url.WithBaseURL("ghcr.io"), url.WithSubPath("my-org"))
// resolver.BasePath() == "ghcr.io/my-org/component-descriptors"

// New opt-in:
resolver, _ := url.New(
    url.WithBaseURL("ghcr.io"),
    url.WithSubPath("my-org"),
    url.WithComponentDescriptorPath(""),
)
// resolver.BasePath() == "ghcr.io/my-org"
// ComponentVersionReference(...) == "ghcr.io/my-org/ocm.software/myapp:v1.2.0"
```

Write-side (CTF):

```go
store := ctf.NewFromCTF(archive, ctf.WithComponentDescriptorPath(""))
// store.ComponentVersionReference(...) == "ctf.ocm.software/ocm.software/myapp:v1.2.0"
```

Read-side (CTF lister) — transparent, no API change:

```go
// An archive written with no prefix is now visible in ListComponents.
// An archive written with component-descriptors/ is still visible.
// Both can coexist in the same archive.
```

#### Spec dependency

A PR to `open-component-model/ocm-spec` must relax the OCI storage backend extension
from:

> The component version is stored at `<base>/<subPath>/component-descriptors/<component>:<version>`

to something like:

> The component version is stored at `<base>/<subPath>[/<prefix>]/<component>:<version>` where `<prefix>` SHOULD be `component-descriptors` for compatibility with existing tooling. Implementations MAY omit `<prefix>` when the registry namespace already provides sufficient isolation.

This code change should be merged only after the spec PR has at minimum been accepted as
a draft.

---

### Option B: Deprecate and flip default

Deprecate `component-descriptors` with a sunset notice in the spec, flip the default
to empty in a future major version of the Go bindings, and require migration tooling.

Pros:

* Cleaner long-term path; removes the legacy suffix from all new component versions
* Reduces confusion for new adopters

Cons:

* Breaking: existing tooling (old CLI versions, third-party implementations) cannot
  read new components without an update
* Requires migration tooling and a coordinated ecosystem release
* Premature: the spec has not yet defined the replacement convention

---

### Option C: Keep mandatory; registry-side aliases

Leave `component-descriptors` mandatory in the spec and code. Address ergonomics by
documenting a registry-level path rewrite pattern (e.g. Nginx proxy, Harbor replication
rules) that strips or rewrites the prefix for end-user display.

Pros:

* Zero code change; zero compatibility risk

Cons:

* Does not solve the underlying friction; just hides it
* Requires each user to operate additional infrastructure
* Puts the burden on non-OCM tooling

---

## Pros and Cons of the Options

### Option A — Configurable opt-in prefix

Pros:

* Zero breaking change; all existing paths continue to work
* Parser already handles both forms — smallest possible delta
* Gives advanced users the ability to use natural OCI paths today
* Sets up a clean migration path toward Option B in a future major version

Cons:

* Two parallel conventions exist in the ecosystem simultaneously
* The `//` double-slash serialisation is awkward for human-readable references
* Requires a spec PR before the feature can be used in a spec-compliant way

### Option B — Deprecate and flip default

Pros:

* Single, clean long-term convention

Cons:

* Breaking; requires coordinated spec + tooling + migration release
* Premature before spec consensus

### Option C — Keep mandatory

Pros:

* No change required

Cons:

* Does not address the root friction
* Inconsistent with standard OCI path conventions

---

## Discovery and Distribution

1. **Spec PR** (prerequisite): open a draft PR on `open-component-model/ocm-spec` relaxing
   the `component-descriptors` requirement from MUST to SHOULD, with an explanation of
   this ADR as motivation.

2. **Code PR** (this repo): implement Option A. Gate the PR on the spec PR being at
   minimum merged as a draft.

3. **CLI integration**: once Option A ships in the library, the `ocm` CLI can expose
   a `--no-component-descriptors-prefix` flag (or repository spec option) for commands
   that push component versions.

4. **Documentation**: update the OCI storage backend guide in `docs/` to document both
   forms and recommend when to use each.

5. **Future**: a separate ADR will address whether and when to flip the default (Option B
   path), informed by ecosystem adoption data and spec consensus.

---

## Conclusion

The `component-descriptors` prefix is a well-intentioned namespace guardrail that has
become a source of friction as OCM matures. The parser and validation layers are already
prefix-agnostic. The only work required is a small, opt-in change to the two write-path
constructors and the CTF lister, plus a spec PR to document the relaxed convention.

This ADR documents the problem, proposes the minimum viable change (Option A), and
surfaces the open angles — serialisation form, spec wording, migration tooling — for
community discussion before implementation begins.
