# OCM Graph Discovery: Code Analysis Notes

## 1. Core Graph Components

### DAG Library (`ocm.software/open-component-model/bindings/go/dag`)
- **Purpose**: Generic DAG implementation used by all OCM packages (constructor, transfer, credentials).
- **Key Features**:
  - Directed acyclic graph with vertex/edge attributes.
  - Thread-safe operations (mutex-protected).
  - Used for recursive discovery in constructor/transfer.
- **Limitations**:
  - No built-in cycle detection.
  - No incremental updates (full re-traversal).
  - No work-stealing or dynamic concurrency.

---

## 2. Constructor Package (`bindings/go/constructor`)

### `discoverer.go`
- **Purpose**: Recursive DAG discovery for component references during construction.
- **Algorithm**:
  - Uses `syncdag.GraphDiscoverer` (wrapper around the DAG library).
  - Resolves components from constructor inputs or external repositories.
  - Discovers child references recursively.
- **Bottlenecks**:
  - Full re-traversal for every construction (no caching).
  - No cycle detection (assumes acyclic graphs).
  - Uses `errgroup` for blob resolution but no work-stealing for DAG traversal.

### `construct.go`
- **Purpose**: Orchestrates component construction.
- **Key Logic**:
  - Builds descriptors from inputs (files, blobs).
  - Uses the discoverer to resolve references.

---

## 3. Transfer Package (`bindings/go/transfer`)

### `internal/discovery.go`
- **Purpose**: Recursive DAG discovery for component transfer.
- **Algorithm**:
  - Uses `dagsync.GraphDiscoverer` (concurrent BFS).
  - Resolves components from source repositories.
  - Propagates targets/resolvers to child components.
- **Bottlenecks**:
  - Full re-traversal for every transfer (no incremental updates).
  - No cycle detection.
  - Resolver ambiguity fails hard (no fallback).

### `internal/graph.go`
- **Purpose**: Builds the transformation graph for transfer.
- **Key Logic**:
  - Generates transformation nodes for each (component, target) pair.
  - Handles resource transfers (local blobs, OCI artifacts, Helm charts).

---

## 4. Credentials Package (`bindings/go/credentials`)

### `graph.go`
- **Purpose**: Resolve credentials for identities (e.g., OCI registries).
- **Algorithm**:
  - Uses a custom DAG (`syncedDag`) for wildcard matching.
  - No recursive discovery (single-identity resolution).
- **Key Features**:
  - Wildcard matching (e.g., `*.docker.io` matches `docker.io`).
  - Cyclic-only edges for wildcard matches.

---

## 5. Common Optimizations Across Packages

| Optimization               | Constructor | Transfer | Credentials | Notes                                                                                     |
|----------------------------|--------------|----------|-------------|-------------------------------------------------------------------------------------------|
| **Incremental Discovery**  | ✅ Yes       | ✅ Yes    | ❌ No        | Cache descriptors; only traverse updates.                                                 |
| **Cycle Detection**        | ✅ Yes       | ✅ Yes    | ⚠️ Partial   | Add Tarjan’s algorithm; credentials already handles "cyclic-only" edges.                |
| **Hybrid BFS**            | ✅ Yes       | ✅ Yes    | ❌ No        | Replace BFS with TopDown/BottomUp traversal.                                              |
| **Resolver Fallback**      | ❌ No        | ✅ Yes    | ❌ No        | Use trust scores or PubGrub for ambiguity resolution.                                     |
| **Work-Stealing**          | ✅ Yes       | ✅ Yes    | ❌ No        | Dynamic concurrency for large graphs.                                                     |

---

## 6. Unified DAG Optimization Strategy

### Goals
- **Single DAG Implementation**: Optimize the underlying DAG library (`ocm.software/open-component-model/bindings/go/dag`).
- **Opt-In Behavior**: Allow packages to enable/disable optimizations (e.g., cycle detection, incremental updates).
- **Backward Compatibility**: Maintain existing APIs.

### Proposed Changes
1. **Cycle Detection**:
   - Add Tarjan’s algorithm to the DAG library.
   - Opt-in via `WithCycleDetection()` option.

2. **Incremental Discovery**:
   - Add caching to the DAG library.
   - Opt-in via `WithIncrementalDiscovery()` option.

3. **Hybrid BFS**:
   - Add TopDown/BottomUp traversal to the DAG library.
   - Opt-in via `WithHybridBFSTraversal()` option.

4. **Work-Stealing**:
   - Add dynamic concurrency to the DAG library.
   - Opt-in via `WithWorkStealing()` option.

---

## 7. Open Questions
1. Should optimizations be **upstreamed to the DAG library** or kept **package-specific**?
2. How should **opt-in behavior** be implemented (e.g., functional options, config structs)?
3. Should the **credentials package** adopt incremental discovery or hybrid BFS?
4. Do you have **sample OCM descriptors/graphs** for benchmarking?