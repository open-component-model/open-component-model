# Agents Guide — Open Component Model (OCM)

Facts for AI agents working in this repo. If a fact here no longer matches the code, fix it.

## Repository Overview

OCM is a multi-module Go monorepo implementing the Open Component Model specification. Two areas matter:

- **`bindings/go/`** — a single Go module: core library packages, the `ocm` CLI (Cobra), and the controller-runtime Kubernetes operator. Package list: `bindings/go/README.md`.
- **`website/`** — Hugo documentation site at <https://ocm.software>. See `website/README.md` and `website/CONTRIBUTING.md`.

Go version: `bindings/go/go.mod`. Build system is **Task**, not Make: <https://taskfile.dev/docs/guide>.

The formal specification OCM implements lives in a separate repo: <https://github.com/open-component-model/ocm-spec>. Treat it as the authoritative source for model semantics (descriptors, signing normalization, CTF/OCI storage).

The library and CLI are **work in progress** — APIs are not yet stable; expect and make breaking changes where warranted rather than preserving a signature for its own sake.

## Commands

Discover all tasks: `task -a`. Most used:

```bash
task test                    # all unit tests, every module
task bindings/go:test        # unit tests for one module (append :test to any module path)
task bindings/go:test -- -run TestFoo   # focus (flags after -- pass through)
task test/integration        # all integration tests (needs Docker / external systems)
task generate                # run all code generators (see Code Generation)
task tidy                    # go mod tidy across all modules
task tools:lint              # golangci-lint on all modules
task tools:lint -- --fix     # auto-fix lint findings
task bindings/go/cli:build   # build ocm CLI into bindings/go/cli/tmp/bin/ocm
```

Controller e2e (own flow): `task bindings/go/kubernetes/controller:test/e2e -- -ginkgo.focus=<scenario>`, with Kind setup/teardown under the same `test/e2e/*` tasks.

Website (from `website/`): `npm ci && npm run dev` serves <http://localhost:1313> live (`npm run dev:drafts` includes drafts); `npm run lint` = eslint + stylelint + markdownlint; `npm test` runs register-docs-version tests. New docs: classify per Diataxis, start from `website/content_templates/` (templates carry the required frontmatter), and use `{{< relref >}}` for internal links — see `website/CONTRIBUTING.md` and `website/README.md`.

## Project Structure

```text
bindings/go/                     # ONE Go module: core libraries + ocm CLI (Cobra) + controller
├── cli/                         # docs/reference/ is generated (see Code Generation)
│   └── integration/             # CLI integration tests — same module, no separate go.mod
├── kubernetes/controller/       # controller-runtime operator
└── runtime/                     # runtime type system (Scheme/Typed)
website/content/                 # live docs; docs/reference/ocm-cli is MOUNTED (generated)
website/content_versioned/       # legacy snapshots — script-generated, never hand-edit
```

Read a package's `doc.go` (if present) and `README.md` before changing it.

## Code Style & Conventions

- **Comments** only when non-obvious: code says *what*, a comment says *why*. Don't narrate added lines.
- **Context**: pass `context.Context` through APIs. Prefer `t.Context()` over `context.Background()`/`.TODO()` in tests (goal for new code; not yet lint-enforced).
- **Runtime type system**: every typed object has a `runtime.Type` (Name + Version) via a `runtime.Scheme`. Walkthrough: [docs/coding-patterns.md#runtime-type-system](docs/coding-patterns.md#runtime-type-system) and the `bindings/go/runtime` package.
- Constructors, error handling, concurrency, JSON marshaling, cleanup: [docs/coding-patterns.md](docs/coding-patterns.md).

## Testing

- **`bindings/go/`**: testify only (`require`, `mock`), no Ginkgo. Table-driven with `t.Run()`; start each test with `r := require.New(t)`. Test data from `testdata/` via `os.ReadFile`/`os.Open` or `//go:embed`.
- **`bindings/go/cli/`**: testify/require. Helper `test.OCM()` (`bindings/go/cli/cmd/internal/test/test.go`) runs CLI commands programmatically. Integration tests in `bindings/go/cli/integration/` — same `bindings/go` module (no separate go.mod), testcontainers.
- **`bindings/go/kubernetes/controller/`**: Ginkgo v2 + Gomega (only area using Ginkgo). Filter with `-ginkgo.focus`, **not** `-run`. Suites self-provision envtest binaries via `internal/test/envtest.go` — no `KUBEBUILDER_ASSETS` setup.
- Logging: `log/slog` via `slog-context` in libraries; `logr` via controller-runtime zap in the controller.
- Idioms: [CLI](docs/coding-patterns.md#cli-idioms), [controller](docs/coding-patterns.md#controller-idioms).

## Git & PR Workflow

- **DCO sign-off mandatory**: commit with `-s`. Every commit ahead of the base must carry a `Signed-off-by:` trailer before push.
- **Conventional Commits** (CI-enforced on PR titles). Types: `feat`, `fix`, `chore`, `docs`, `test`, `perf`. Breaking: `!`. Use `chore:`, never `ci:`.

  ```text
  Good:  feat(cli): add component version command
  Good:  fix(repository): handle nil pointer in resolver
  Bad:   updated some stuff            (no type, vague)
  Bad:   ci: fix workflow              (use chore:)
  ```

- **Keep a PR focused** — one logical change. `bindings/go` is the only buildable Go module; CI selects work by path filters, not by module.
- **Commit body says *why*** (motivation, root cause), not *what* — the diff shows what. Don't list changed files.
- After push verify CI: `gh pr checks <number>` (`--watch` to block). Don't claim green without evidence.
- **No interactive git** — the agent shell has no TTY. Use `-m` / `--no-edit`, never `-i` (`rebase -i`, bare `commit`, `merge`) or it hangs.

## Boundaries

- **Before finishing**: run the affected area's lint + tests (`task tools:lint`, `task <module>:test`).
- **Ask first**: adding a dependency; changing a public API, CRD, or reconciliation path with broad blast radius; force-pushing (destructive).
- **Never commit** secrets or credentials.
- **Never hand-edit generated output** — run `task generate` and commit the result. Covers deepcopy, controller manifests, the **CLI reference** (`bindings/go/cli/docs/reference/` → mounted at `content/docs/reference/ocm-cli/`), and **JSON schemas** (generated from `bindings/go/` into the website's `static/schemas/`). The other `content/docs/reference/*.md` are hand-authored prose you edit directly — but they embed the generated schemas via a `{{< schema-renderer >}}` shortcode, so to change a documented schema field, edit the Go source and regenerate, not the page.
- **Never hand-edit** `website/content_versioned/` (legacy, generated) or Hugo version configs (`website/config/_default/hugo.yaml`, `module.yaml` — use `npm run register-docs-version -- x.y.z`).

## Security

- Credential handling and signing/verification are load-bearing: ADR 0002 (credentials), ADR 0008 (signing) in `docs/adr/`.
- **Controller performance**: for code touching many objects, weigh watch/list efficiency, reconcile cost, memory, caching.

## Keep in Sync

Coupled files — editing one without the other breaks CI or behavior:

- **Tool/binary versions live in per-area `.env` files (renovate-managed)**: root `.env` (golangci-lint, deepcopy-gen, markdownlint-cli2), `bindings/go/kubernetes/controller/.env` (controller-tools, envtest, kind node), `bindings/go/sigstore/signing/handler/internal/.env` (cosign), `bindings/go/sigstore/integration/.env` (scaffolding). Taskfiles source these — never hardcode a version in a Taskfile or script. Cross-dir coupling: `sigstore/integration/Taskfile.yml` reads `COSIGN_VERSION` from `signing/handler/internal/.env`.
- `ENVTEST_K8S_VERSION` (`controller/.env`) ↔ `DefaultEnvTestVersion` (`.../internal/test/envtest.go`) — keep equal.
- Docs version ↔ `hugo.yaml` + `module.yaml` — only via `npm run register-docs-version`.
- Website Node/npm floors — check the `engines` field in `website/package.json`, don't restate here.

## Code Generation

`task generate` runs every generator (ocm type gen, JSON schema gen, deepcopy, controller manifests + deepcopy, CLI reference docs) then `task tidy`. Run after changing any generation marker and commit the output. `task -a` lists individual generators.

## Dependencies & CI

- Renovate manages updates. Manual add: `go get <module>@<version> && task tidy`.
- CI selects work by path filter (`dorny/paths-filter`), not module discovery: unit + integration tests run when `bindings/go`, the shared task plumbing, or `ci.yml` changes; lint runs for the changed module, or all modules when `golangci.yml` / `.env` / `ci.yml` changes. Full suite on `main`. PR title must be a valid Conventional Commit. Multi-arch builds (linux/darwin, amd64/arm64) for CLI and controller.

## Debugging

```bash
ocm --loglevel debug <command>                    # CLI debug logging
ocm get componentversion <component> -o yaml      # inspect descriptors
./my-plugin server --config='...' 2>&1 | tee plugin.log   # plugin logs
```
