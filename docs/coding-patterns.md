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

### Builder / Method Chaining

Some packages use fluent builders where `With*()` methods return `*Builder` for chaining:

```go
b := NewBuilder(scheme).
    WithTransformer(t).
    WithEvents(ch)
result, err := b.Build(ctx)
```

---

## Type Definitions

### Typed String Enums

String-based enums use a named type with a const block:

```go
type Policy string

const (
    PolicyAllow Policy = "Allow"
    PolicyDeny  Policy = "Deny"
)
```

Validation happens at the boundary — custom flag types validate on `Set()`, kubebuilder markers validate on admission.

### Iota Enums

Numeric enums use `iota`:

```go
type CopyMode int

const (
    CopyModeLocal CopyMode = iota
    CopyModeAll
)
```

### Version Constants

Each typed package defines its type name and version as constants:

```go
const (
    Type    = "example.config"
    Version = "v1"
)
```

These are passed to `runtime.NewVersionedType(Type, Version)` during scheme registration.

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

### Capability / Trait Interfaces

Optional behaviors are expressed as small single-method interfaces that a type may or may not implement. Consumers use type assertions to discover capabilities at runtime:

```go
type SizeAware interface {
    Size() (int64, error)
}

type DigestAware interface {
    Digest() (string, error)
}

// Consumer checks at runtime:
if sa, ok := blob.(SizeAware); ok {
    size, _ := sa.Size()
    // use size
}
```

This pattern avoids bloating the primary interface with optional concerns.

### Callback / Hook Function Fields

Structs use function fields for lifecycle extensibility. Callbacks follow the `On<Event>` naming convention:

```go
type Callbacks struct {
    OnStart func(ctx context.Context, obj *Thing) error
    OnEnd   func(ctx context.Context, obj *Thing, err error) error
}
```

### Adapter / Converter Wrappers

Unexported structs adapt between interface boundaries (e.g., external plugin contracts to internal interfaces). The wrapper holds the external dependency and translates method signatures:

```go
type pluginConverter struct {
    external ExternalContract
    scheme   *runtime.Scheme
}

var _ InternalInterface = (*pluginConverter)(nil)
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

### Deferred Error Composition

Combine multiple errors from cleanup paths with `errors.Join()`:

```go
func doWork() (err error) {
    r, err := open()
    if err != nil {
        return err
    }
    defer func() { err = errors.Join(err, r.Close()) }()
    // ...
}
```

Also used inline:

```go
return errors.Join(ErrUnknown, fmt.Errorf("operation failed: %w", err))
```

### Custom Error Types

Domain-specific error types carry structured context and enable `errors.As()` matching:

```go
type NotReadyError struct {
    ObjectName string
}

func (e *NotReadyError) Error() string {
    return fmt.Sprintf("object %s is not ready", e.ObjectName)
}
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

### Closure-Based Lock Guards

Wraps critical sections in closures to centralize lock management:

```go
func (g *SyncedGraph[T]) WithReadLock(fn func(d *Graph[T]) error) error {
    g.mu.RLock()
    defer g.mu.RUnlock()
    return fn(g.graph)
}
```

### sync.OnceValues for Lazy Initialization

Defers expensive setup (environment creation, client initialization) to first use:

```go
var getEnv = sync.OnceValues(func() (*Environment, error) {
    return createExpensiveEnvironment()
})
```

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

## JSON Marshaling

### Custom Marshal/Unmarshal for Backward Compatibility

Types that need to accept multiple input formats use the type-alias trick to avoid infinite recursion:

```go
func (c *Consumer) UnmarshalJSON(data []byte) error {
    type Alias Consumer
    alias := &Alias{}
    if err := json.Unmarshal(data, alias); err == nil {
        *c = Consumer(*alias)
        return nil
    }
    // try legacy format...
}
```

Some types accept both array and single-object forms, trying each format and falling back:

```go
func (c *Spec) UnmarshalJSON(data []byte) error {
    var items []Item
    if err := json.Unmarshal(data, &items); err == nil {
        c.Items = items
        return nil
    }
    var single Item
    if err := json.Unmarshal(data, &single); err == nil {
        c.Items = []Item{single}
        return nil
    }
    return fmt.Errorf("unable to unmarshal spec")
}
```

### Schema Embedding

JSON schemas for validation are embedded in production code via `//go:embed`:

```go
//go:embed schemas/Config.schema.json
var configSchema []byte
```

Generated by `jsonschemagen` into `zz_generated.ocm_jsonschema.go` files.

---

## Receiver Conventions

- **Pointer receivers** for methods that mutate state or implement `UnmarshalJSON`.
- **Value receivers** for pure queries like `String()`, `IsZero()`, `Equal()`.

```go
func (t Type) String() string     { /* value - no mutation */ }
func (t *Type) UnmarshalJSON([]byte) error { /* pointer - mutates */ }
```

Most structs use pointer receivers throughout. Value receivers are reserved for small value types.

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

### Iterator-Based Lazy Evaluation (iter.Seq2)

Go range-over-func iterators for lazy traversal without materializing collections:

```go
func (r *Scheme) GetTypesIter() iter.Seq2[Type, iter.Seq[Type]] {
    return func(yield func(Type, iter.Seq[Type]) bool) {
        r.mu.RLock()
        defer r.mu.RUnlock()
        for typ := range r.defaults.Iter() {
            if !yield(typ, r.AliasesIter(typ)) {
                return
            }
        }
    }
}
```

Used alongside materialized methods (e.g., `GetTypes()`) for flexibility.

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

### Blank Imports for Side-Effect Registration

Packages that self-register types via `init()` are imported with blank identifiers where their types are needed:

```go
import _ "ocm.software/open-component-model/bindings/go/access/localblob/v1"
```

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

### Custom Flag Types

Flags implement `pflag.Value` for validation at set-time. Reusable flag types (enum, file) enforce constraints and generate help text automatically:

```go
enum.VarP(cmd.Flags(), "output", "o", []string{"json", "yaml", "ndjson"}, "output format")
```

File flags validate existence on `Set()` and expose `Open()` / `Exists()` helpers.

### Output Formatting

Pluggable renderer system supporting JSON, YAML, NDJSON, Tree, and Table output via a format enum. Two render modes: static (one-time) and live (terminal refresh with ANSI control sequences). Pager integration for large output.

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

Helpers abstract fluxcd condition management. Condition helpers combine condition mutation with event recording in a single call:

```go
status.MarkReady(recorder, obj, "reconciled")
status.MarkNotReady(recorder, obj, reason, message)
status.MarkAsStalled(recorder, obj, reason, message)
```

The deferred `UpdateStatus` observes reconciliation state — setting `ProgressingWithRetryReason` on errors during reconciliation and mutating `ObservedGeneration` only when ready.

### Predicates

All controllers use `GenerationChangedPredicate` to only reconcile on spec changes, filtering out status-only updates:

```go
For(&v1alpha1.Component{}, builder.WithPredicates(predicate.GenerationChangedPredicate{}))
```

### Field Indexing and Cross-Resource Watches

Field indexes are registered at controller setup for efficient cross-resource lookups. Watch handlers use `handler.EnqueueRequestsFromMapFunc` with `client.MatchingFields{}` to find related objects:

```go
Watches(&v1alpha1.Repository{},
    handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
        list := &v1alpha1.ComponentList{}
        r.List(ctx, list, client.MatchingFields{fieldName: obj.GetName()})
        // build and return requests...
    }))
```

### Finalizers

Deletion is guarded by finalizers. `reconcileDelete` checks for dependent resources before removing the finalizer. Adding a finalizer triggers an immediate requeue. Multiple finalizers may be used in sequence to enforce cleanup ordering.

### Server-Side Apply with ApplySet

The deployer uses SSA (`client.Apply` with `client.ForceOwnership`) and ApplySet (KEP-3659) for resource lifecycle management. The workflow is: Project (compute scope) → Apply (SSA all resources) → Prune (delete orphans matching the ApplySet label).

### Worker Pool with Cache

Async resolution uses a worker pool with an expirable LRU cache. The `Load(key, fallbackFunc)` pattern checks cache first, then dispatches work. Multiple requesters can subscribe to the same in-progress resolution:

```go
result, err := cache.Load(key, func() (V, error) {
    return expensiveOperation()
})
```

### Owner References

Controller references are set on dynamically deployed objects for garbage collection. Dynamic informers use `EnqueueRequestForOwner` with `OnlyControllerOwner()` to watch owned resources.

### CRD Types

Defined with kubebuilder markers. List types and scheme registration happen in `init()`.

### Dynamic Informer Management

For watching arbitrary GVKs at runtime, a custom informer manager maintains metadata-only caches (`PartialObjectMetadata`). Register/unregister via channels. Implements `manager.Runnable` for controller-runtime integration. A transformer strips objects to metadata-only to reduce memory.

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
