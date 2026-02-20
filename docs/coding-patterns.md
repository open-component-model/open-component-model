# OCM Coding Patterns

Idiomatic Go patterns and conventions used across the OCM monorepo.

---

## Constructor Patterns

### Functional Options

The dominant constructor pattern. Define an option type as a function that mutates a config struct, apply via variadic `New*()`.

```go
type Option func(*Options)

func WithScheme(scheme *runtime.Scheme) Option {
    return func(o *Options) {
        o.Scheme = scheme
    }
}

func New(opts ...Option) (*Thing, error) {
    o := &Options{}
    for _, opt := range opts {
        opt(o)
    }
    if o.Scheme == nil {
        o.Scheme = DefaultScheme
    }
    // ...
}
```

Nil fields are always checked after applying options and replaced with sensible defaults.

### Simple Constructors

Used when no configuration variability is needed. `New*()` returns a pointer to the struct.

```go
func NewManager(ctx context.Context) *Manager {
    return &Manager{
        Registry: NewRegistry(ctx),
    }
}
```

---

## Interface Design

### Small, Composable Interfaces

Interfaces are kept small — typically 1–3 methods. Larger interfaces are composed via embedding.

```go
type Signer interface {
    Sign(ctx context.Context, ...) (Signature, error)
}

type Verifier interface {
    Verify(ctx context.Context, ...) error
}

type Handler interface {
    Signer
    Verifier
}
```

Modules may extend base interfaces from other modules with domain-specific methods:

```go
type ExtendedRepository interface {
    repository.Repository
    repository.HealthCheckable
    DigestProcessor
}
```

---

## Error Handling

### Sentinel Errors

Package-level, `Err` prefix, `errors.New()`:

```go
var ErrNotFound = errors.New("not found")
```

### Wrapping

Wrap with `fmt.Errorf` and `%w`. Message describes the failed operation:

```go
return fmt.Errorf("unable to open file: %w", err)
return fmt.Errorf("failed to resolve version: %w", err)
```

### Joining

Combine multiple errors with `errors.Join()`:

```go
return errors.Join(ErrUnknown, fmt.Errorf("operation failed: %w", err))
```

### Checking

Always `errors.Is()` for sentinels, `errors.As()` for typed errors:

```go
if errors.Is(err, ErrNotFound) {
    // handle
}
```

### Terminal vs Retriable (Controller)

Reconcilers wrap non-retriable errors with `reconcile.TerminalError()` to prevent requeue:

```go
return reconcile.TerminalError(fmt.Errorf("invalid config: %w", err))
```

---

## Context

`context.Context` is always the first parameter in public API methods. No exceptions.

```go
func (r *Repo) Get(ctx context.Context, name string) (*Thing, error)
```

In tests, use `t.Context()` (bindings/CLI) or `ctx SpecContext` (controller/Ginkgo). Never `context.Background()` or `context.TODO()`.

---

## Concurrency

### sync.RWMutex

For registries and caches with many readers, few writers.

### sync.Mutex

For exclusive access. Always `defer Unlock()` immediately after `Lock()`.

```go
mu.Lock()
defer mu.Unlock()
```

### errgroup

For parallel operations with error aggregation:

```go
g, ctx := errgroup.WithContext(ctx)
g.Go(func() error { /* ... */ })
if err := g.Wait(); err != nil {
    return err
}
```

### sync.Map

For lock-free concurrent access in hot paths (e.g., done-tracking maps).

---

## Resource Cleanup

### Defer with Named Returns

Capture final error state for logging or metrics:

```go
func (r *Repo) Do(ctx context.Context) (err error) {
    done := log.Operation(ctx, "doing thing")
    defer func() { done(err) }()
    // ...
}
```

### io.Closer Adapter

Wrap cleanup functions as `io.Closer`:

```go
type closerFunc func() error
func (f closerFunc) Close() error { return f() }
```

### Compile-Time Interface Assertion

```go
var _ io.Closer = (*MyType)(nil)
```

---

## Generics

Used sparingly — primarily in DAG processing and generic utility functions.

```go
func Process[K cmp.Ordered, V any](ctx context.Context, graph *Graph[K]) error
```

Controller utilities use generics with pointer type constraints for K8s objects:

```go
func GetReadyObject[T any, P ObjectPointer[T]](ctx context.Context, c client.Reader, key client.ObjectKey) (P, error)
```

---

## Runtime Type System

The runtime type system is the foundation of OCM. It lives in `bindings/go/runtime/` and provides the mechanism by which all typed objects are identified, registered, serialized, deserialized, and converted.

### runtime.Type

A name + optional version pair:

```go
type Type struct {
    Version string
    Name    string
}
```

Helpers:

```go
runtime.NewVersionedType("file", "v1")    // Type{Name: "file", Version: "v1"}
runtime.NewUnversionedType("file")        // Type{Name: "file", Version: ""}
```

`Type.String()` returns `"file"` (unversioned) or `"file/v1"` (versioned). JSON marshals to the same string representation. `UnmarshalJSON` accepts both `"file/v1"` and `{"type": "file/v1"}` as input.

### runtime.Typed Interface

Every object in the type system must implement:

```go
type Typed interface {
    GetType() Type
    SetType(Type)
    DeepCopyTyped() Typed
}
```

- `GetType()` / `SetType()` — auto-generated via `// +ocm:typegen=true`
- `DeepCopyTyped()` — auto-generated via `// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed`

A concrete implementation:

```go
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type Config struct {
    Type      runtime.Type `json:"type"`
    Path      string       `json:"path"`
}
```

The generated code produces `zz_generated.ocm_type.go` (GetType/SetType) and `zz_generated.deepcopy.go` (DeepCopy/DeepCopyTyped).

### runtime.Scheme

Thread-safe registry mapping `Type` ↔ `reflect.Type`. Central mechanism for creating, resolving, decoding, and converting typed objects.

Internal structure:

```go
type Scheme struct {
    mu           sync.RWMutex
    allowUnknown bool                           // if true, unknown types resolve to Raw{}
    defaults     *bimap.Map[Type, reflect.Type] // bidirectional 1:1 mapping
    aliases      map[Type]Type                  // alias → default type
    instances    map[reflect.Type]Typed         // prototype instances for DeepCopy
}
```

The `defaults` bimap provides O(1) lookup in both directions. Aliases map alternative Type values to the canonical default.

#### Creating a Scheme

```go
scheme := runtime.NewScheme()
scheme := runtime.NewScheme(runtime.WithAllowUnknown())  // unknown types resolve to Raw{}
```

#### Registering Types

**`MustRegisterWithAlias`** — primary registration method. First Type is the default, rest are aliases:

```go
scheme.MustRegisterWithAlias(&v1.Config{},
    runtime.NewVersionedType("config", "v1"),   // default
    runtime.NewUnversionedType("config"),        // alias
)
```

What happens internally:
1. `defaults` bimap stores: `Type{Name:"config", Version:"v1"}` ↔ `reflect.TypeOf(&v1.Config{})`
2. `aliases` stores: `Type{Name:"config", Version:""}` → `Type{Name:"config", Version:"v1"}`
3. `instances` stores: `reflect.TypeOf(&v1.Config{})` → `&v1.Config{}` (the prototype, via `DeepCopyTyped()`)

Panics if a Type or reflect.Type is already registered. Use `RegisterWithAlias` for the error-returning variant.

**`MustRegister`** — derives the type name from the Go struct name:

```go
scheme.MustRegister(&Config{}, "v1")
// Registers as Type{Name: "Config", Version: "v1"}
```

**`RegisterScheme`** — merges all types from another Scheme:

```go
scheme.RegisterScheme(otherScheme)
```

#### Creating Instances

```go
obj, err := scheme.NewObject(runtime.NewVersionedType("config", "v1"))
```

Resolution path:
1. Look up in `defaults` bimap → found? Use that reflect.Type
2. Not found → look up in `aliases` → get the default Type → look up in `defaults`
3. Get the prototype from `instances[reflect.Type]`
4. Call `prototype.DeepCopyTyped()` to get a fresh instance
5. Call `obj.SetType(requestedType)` — sets the Type to what was requested (including aliases)
6. Return the instance

If the Type is not found and `allowUnknown` is false, returns an error. If `allowUnknown` is true, returns `&Raw{}`.

#### Decoding

```go
err := scheme.Decode(reader, into)
```

Reads YAML/JSON from `io.Reader`, unmarshals into the target. If the target already has a non-empty Type before decoding, validates that the decoded Type matches.

#### Converting

`Convert()` handles four cases:

| From | To | What Happens |
|------|----|-------------|
| `*Raw` | `*Raw` | Deep copy |
| `*Raw` | Typed struct | `json.Unmarshal(raw.Data, into)` |
| Typed struct | `*Raw` | `json.Marshal(from)` → canonicalize → store in `Raw.Data` |
| Typed struct | Typed struct | `DeepCopyTyped()` + reflection assignment |

### runtime.Raw

Wraps unknown or extensible types. Holds the Type and canonical JSON bytes without interpreting them:

```go
type Raw struct {
    Type `json:"type"`
    Data []byte `json:"-"`
}
```

- `UnmarshalJSON`: extracts `"type"`, stores full JSON in `Data` (canonicalized)
- `MarshalJSON`: returns `Data` directly

Use case — fields that can hold any typed object:

```go
type Resolver struct {
    Repository *runtime.Raw `json:"repository"`
}
```

Convert to concrete type via `scheme.Convert(&raw, &concreteType)`.

### runtime.Identity

A `map[string]string` that uniquely identifies resources. Implements `Typed` (GetType/SetType via the `"type"` key). Provides `CanonicalHashV1()` (FNV64 of sorted key=value pairs) and stable `String()`.

Reserved keys: `type`, `hostname`, `scheme`, `path`, `port`.

### Registration Patterns

#### Single Scheme Per Package

```go
var Scheme = runtime.NewScheme()

func init() {
    Scheme.MustRegisterWithAlias(&v1.Config{},
        runtime.NewVersionedType(Type, Version),
        runtime.NewUnversionedType(Type),
    )
}
```

#### MustAddToScheme for External Consumers

Expose a `MustAddToScheme` function so external packages can pull in your types:

```go
func MustAddToScheme(scheme *runtime.Scheme) {
    scheme.MustRegisterWithAlias(&v1.Config{},
        runtime.NewVersionedType(Type, Version),
        runtime.NewUnversionedType(Type),
    )
}
```

#### Scheme Composition

Merge multiple sub-schemes into a parent:

```go
func Register(scheme *runtime.Scheme) error {
    return scheme.RegisterSchemes(
        specv1.Scheme,
        configv1.Scheme,
    )
}
```

Or consumer-side via `init()`:

```go
var DefaultScheme = runtime.NewScheme()

func init() {
    pkg1.MustAddToScheme(DefaultScheme)
    pkg2.MustAddToScheme(DefaultScheme)
}
```

### End-to-End Type Flow

JSON → Raw → Typed → modify → Raw → JSON:

```go
// 1. JSON arrives
data := []byte(`{"type": "config/v1", "host": "localhost"}`)

// 2. Unmarshal into Raw (preserves JSON, extracts Type)
var raw runtime.Raw
json.Unmarshal(data, &raw)

// 3. Convert Raw → concrete type via Scheme
cfg := &v1.Config{}
scheme.Convert(&raw, cfg)

// 4. Modify
cfg.Host = "example.com"

// 5. Convert back to Raw
out := &runtime.Raw{}
scheme.Convert(cfg, out)

// 6. Serialize
output, _ := json.Marshal(out)
```

### Common Mistakes

**Forgetting to register a type.** `Convert()` and `NewObject()` return errors for unregistered types. Always register in `init()` or via `MustAddToScheme`.

**Registering the same type twice.** `MustRegisterWithAlias` panics if the Type value or Go type is already registered. Use one call with multiple aliases:

```go
// Wrong — panics on second call
scheme.MustRegisterWithAlias(&Config{}, runtime.NewVersionedType("config", "v1"))
scheme.MustRegisterWithAlias(&Config{}, runtime.NewVersionedType("config", "v2"))

// Correct — one call, multiple aliases
scheme.MustRegisterWithAlias(&Config{},
    runtime.NewVersionedType("config", "v1"),
    runtime.NewVersionedType("config", "v2"),
)
```

**DeepCopyTyped returning self.** The Scheme stores prototypes and clones them via `DeepCopyTyped()`. Returning the same pointer causes shared mutable state:

```go
// Wrong
func (m *MyType) DeepCopyTyped() runtime.Typed { return m }

// Correct
func (m *MyType) DeepCopyTyped() runtime.Typed {
    c := *m
    return &c
}
```

**Alias resolution preserves the requested Type.** `NewObject()` calls `SetType()` with the Type you asked for, not the canonical default. If you look up an unversioned alias, `GetType()` returns the unversioned form.

**Decode validates pre-set types.** If the target already has a non-empty Type before `Decode()` and the JSON contains a different type, Decode returns an error.

---

## Package Organization

### Internal Packages

Implementation details live under `internal/`. Public interfaces are defined in the parent package.

### File Naming

| Pattern | Purpose |
|---------|---------|
| `zz_generated.*.go` | Generated code — never edit |
| `doc.go` | Package documentation |
| `interface.go` | Interface definitions |
| `*_options.go` | Functional options |
| `suite_test.go` | Ginkgo test suite setup |

---

## Import Order

Enforced by `gci`. Four groups separated by blank lines:

1. Standard library
2. Blank imports (`_ "embed"`)
3. Third-party packages
4. OCM modules (`ocm.software/open-component-model/...`)

```go
import (
    "context"
    "fmt"

    _ "embed"

    "github.com/spf13/cobra"

    "ocm.software/open-component-model/bindings/go/runtime"
)
```

---

## Testing

### Framework by Area

| Area | Framework | Assertion Style |
|------|-----------|----------------|
| `bindings/go/` | testify | `require.New(t)` |
| `cli/` | testify + `test.OCM()` helper | `require.New(t)` |
| `kubernetes/controller/` | Ginkgo v2 + Gomega | `Expect().To()` |

### Table-Driven Tests (bindings/CLI)

```go
tests := []struct {
    name    string
    input   string
    want    Type
    wantErr bool
}{...}
for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        r := require.New(t)
        // ...
    })
}
```

Start every test function with `r := require.New(t)`.

### Ginkgo (Controller)

```go
var _ = Describe("Controller", func() {
    BeforeEach(func() { /* setup */ })

    It("does something", func(ctx SpecContext) {
        By("step description")
        // ...
    })
})
```

Async assertions use `Eventually` with context:

```go
Eventually(func(ctx context.Context) error {
    return k8sClient.Get(ctx, key, obj)
}, "15s").WithContext(ctx).Should(Succeed())
```

### Test Data

- `//go:embed testdata` for static fixtures
- `t.TempDir()` / `GinkgoT().TempDir()` for ephemeral data

---

## CLI Idioms

### Command Construction

Every command is a `New()` function returning `*cobra.Command`. Parent commands return `cmd.Help()` from `RunE`. Subcommands are added via `cmd.AddCommand()`.

### Dependency Injection

Dependencies are injected via context during `PersistentPreRunE` and retrieved with typed accessors:

```go
ocmctx.FromContext(cmd.Context()).PluginManager()
```

### Custom Flags

Flags implement `flag.Value` for validation at set-time (e.g., enum flags that reject invalid values).

### Output Formatting

Pluggable renderer system supporting JSON, YAML, NDJSON, Tree, and Table output via a format enum.

---

## Controller Idioms

### Reconciler Structure

All reconcilers embed a shared base reconciler providing `ctrl.Client`, `runtime.Scheme`, and `record.EventRecorder`.

### Reconcile Signature

Named error return enables deferred status updates that capture the final error state:

```go
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, err error) {
    patchHelper := patch.NewSerialPatcher(obj, r.Client)
    defer func(ctx context.Context) {
        err = errors.Join(err, status.UpdateStatus(ctx, patchHelper, obj, r.EventRecorder, obj.GetRequeueAfter(), err))
    }(ctx)
    // ...
}
```

### Status Conditions

Helpers abstract fluxcd condition management:

```go
status.MarkReady(recorder, obj, "reconciled")
status.MarkNotReady(recorder, obj, reason, message)
status.MarkAsStalled(recorder, obj, reason, message)
```

### Field Indexing

Cross-resource lookups use field indexes registered at controller setup, queried via `client.MatchingFields{}` in watch handlers.

### Finalizers

Deletion is guarded by finalizers. `reconcileDelete` checks for dependent resources before removing the finalizer. Adding a finalizer triggers an immediate requeue.

### CRD Types

Defined with kubebuilder markers. List types and scheme registration happen in `init()`.

### Metrics

Registered via helper functions organized by subsystem (`MustRegisterCounterVec`, `MustRegisterGauge`, `MustRegisterHistogramVec`).

---

## Logging

| Area | Logger |
|------|--------|
| `bindings/go/` | `log/slog` via `slogcontext` |
| `cli/` | `log/slog` with JSON/text format flag |
| `kubernetes/controller/` | `logr` via controller-runtime zap |

---

## Code Generation

Three generators, triggered by markers:

| Generator | Marker | Output |
|-----------|--------|--------|
| ocmtypegen | `// +ocm:typegen=true` | `zz_generated.ocm_type.go` |
| jsonschemagen | `// +ocm:jsonschema-gen=true` | `zz_generated.ocm_jsonschema.go` |
| deepcopy-gen | `// +k8s:deepcopy-gen=true` | `zz_generated.deepcopy.go` |

All generated files carry `//go:build !ignore_autogenerated`. Run `task generate` after adding or modifying markers.
