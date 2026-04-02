# Typed Credentials and Consumer Identity Types

* Status: proposed
* Deciders: Matthias Bruns, Gergely Brautigam, Fabian Burth, Jakob Moeller
* Date: 2026.04.01

Technical Story: Evolve the OCM credential system from untyped `map[string]string` credentials into a type-safe, self-documenting system that validates credential and identity types at both configuration time and consumption time.

## Context and Problem Statement

The credential graph (see [ADR 0002](0002_credentials.md)) resolves credentials for consumer identities through a DAG. The resolution model is sound, but credentials and identities are untyped:

- **Credentials are `map[string]string`** — key names like `username`, `password`, `accessToken` are scattered string constants with no compile-time guarantees. A real bug existed where OCI resource downloads used `access_token` (snake_case) while docker config resolution used `accessToken` (camelCase), causing silent auth failures.

- **Consumer identity types are scattered strings** — `"OCIRegistry"`, `"HelmChartRepository"`, `"RSA/v1alpha1"` defined independently per binding with no central registry, inconsistent versioning, and no way to enumerate them.

- **No validation of identity ↔ credential compatibility** — configuring RSA credentials for a Helm identity produces no warning. Users have no way to discover what credentials each identity type accepts.

- **No credential type specialization** — a Helm HTTP repository needs `certFile`/`keyFile`/`keyring`, while an OCI-backed Helm repository needs `username`/`password`/`accessToken`. Both use the same generic map today, making invalid combinations representable.

## Decision Drivers

1. **Type safety** — Invalid credential fields caught at compile time, not runtime
2. **Validation** — Mismatched identity/credential pairs detected at configuration time
3. **Discoverability** — Users and tooling can enumerate identity types, their accepted credential types, and required fields
4. **Backward compatibility** — Existing `.ocmconfig` files continue to work unchanged
5. **Gradual migration** — Multi-module monorepo requires non-blocking, per-binding migration
6. **Extensibility** — Plugins can register custom types without collisions

## Decision Outcome

### Typed Credential and Identity Specs

Each binding defines typed Go structs for its credentials and identities, registered in `runtime.Scheme` registries. The type system enforces valid credential shapes — for example, Helm HTTP credentials have `CertFile`/`KeyFile`/`Keyring` fields, while OCI credentials have `AccessToken`/`RefreshToken`. Invalid combinations are unrepresentable.

Where a single consumer supports multiple credential shapes (e.g., Helm supports both HTTP and OCI repositories), separate credential types are defined per access mode rather than one type with all fields.

### Identity → Credential Type Validation

Typed identity structs declare which credential types they accept:

```go
type CredentialAcceptor interface {
    AcceptedCredentialTypes() []runtime.Type
}
```

The graph validates during ingestion that configured credential types are compatible with the identity type. Incompatible pairs produce warnings. Consumers reject credentials of the wrong type with clear errors.

### Resolver Evolution

The existing `Resolver` interface gains a `ResolveTyped` method that returns `runtime.Typed` instead of `map[string]string`. The graph stores credentials as `runtime.Typed` internally and resolves typed credentials from config when a `CredentialTypeScheme` is provided. `DirectCredentials/v1` serves as the fallback for old configurations.

Adding a method to an interface only breaks implementors (all in our codebase), not consumers. Each binding migrates from `Resolve` to `ResolveTyped` independently.

### Identity as Builder, Map for Matching

Typed identity structs implement `IdentityProvider` to produce `runtime.Identity` maps. The graph continues to use the map representation internally for its matching semantics (wildcard paths, URL normalization, port defaulting). Typed structs are builders and validators, not replacements for the map.

### Backward Compatibility

- `.ocmconfig` format is unchanged — `Credentials/v1` with `properties` continues to work
- `DirectCredentials/v1` is the universal fallback, registered with all aliases
- Each typed credential provides a `FromDirectCredentials` converter
- Unversioned identity types work through `runtime.Scheme` alias resolution
- External plugin wire format stays `map[string]string`

## Migration Path

The OCM codebase is a multi-module Go monorepo where each binding has its own `go.mod`. Interface changes cascade across module boundaries. Without `go.work`, modules resolve from the proxy — so changes must be published in dependency order.

### Phase 1: Foundation

Add `ResolveTyped` to `Resolver`, typed credential/identity scheme support to the graph, and `IdentityProvider`/`CredentialAcceptor` interfaces to runtime. No downstream breakage — all existing code continues to work.

### Phase 2: Binding migration (parallelizable)

Each binding creates its typed credential and identity specs, migrates internal code to use `ResolveTyped`, and rejects incompatible credential types. Bindings can be migrated independently in separate PRs.

### Phase 3: Plugin interfaces

Update `CredentialPlugin` and `RepositoryPlugin` interfaces to accept and return `runtime.Typed`. Plugin HTTP transport converts at the wire boundary.

### Phase 4: Repository interfaces

Once all bindings work with typed credentials, update `ResourceRepository`, `ComponentVersionRepositoryProvider`, `ResourceDigestProcessor`, `Signer`/`Verifier`, and constructor interfaces to accept `runtime.Typed`.

### Phase 5: Consumer migration

CLI commands, K8s controller, and remaining consumers switch from `Resolve` to `ResolveTyped`.

### Phase 6: Cleanup

Deprecate `Resolve` method. Remove internal map conversion helpers and legacy credential key constants.

### Key Constraints

- Module publish order matters — each phase must be merged and published before downstream phases.
- Phase 2 PRs can run in parallel across bindings.
- Phase 4 blocks on Phase 2 completion.
- Phase 6 is the only step that removes backward compatibility.
- Old `.ocmconfig` files work at every stage.

## POC Validation

Branch `feat/800_typed_credentials_poc` validates the design end-to-end using Helm as the reference binding:

- Typed `HelmHTTPCredentials/v1` and `HelmChartRepositoryIdentity/v1` specs
- Graph resolves typed credentials from config via `CredentialTypeScheme`
- `CredentialAcceptor` validation at the consumer level — incompatible types rejected with errors
- Full test coverage: spec unit tests, graph integration tests, CLI integration tests
- Backward compatibility verified with old `Credentials/v1` configs
- Download library fully migrated to typed credentials internally

## Conclusion

The typed credential system makes invalid credential configurations unrepresentable through Go's type system. Each binding owns its credential and identity types. The graph validates and stores typed credentials natively. The gradual migration path ensures no development blocking while transitioning the multi-module monorepo.
