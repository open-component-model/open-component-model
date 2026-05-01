# OCM Graph Discovery: Research Findings

## 1. Incremental Graph Algorithms
- **Key Idea**: Only traverse/update changed portions of the graph (e.g., new/updated components).
- **Papers/Tools**:
  - [GraphFly: Dependency-Aware Streaming Graph Processing (arXiv, 2025)](https://arxiv.org/html/2507.11094v1)
  - [Almost-Linear Time Algorithms for Incremental Graphs (arXiv, 2023)](https://arxiv.org/pdf/2311.18295)
- **OCM Relevance**:
  - Cache resolved descriptors and only re-traverse updated components.
  - Requires tracking "last updated" timestamps or hashes.

---

## 2. Parallel BFS Optimizations
- **Key Idea**: Use hybrid TopDown/BottomUp traversal and dynamic partitioning to reduce contention.
- **Papers/Tools**:
  - [BFSBlitz: Highly Parallel Graph System for BFS (ACM, 2025)](https://dl.acm.org/doi/10.1145/3711708.3723444)
  - [Parallel BFS on Distributed Memory Systems (ACM, 2011)](https://dl.acm.org/doi/10.1145/2063384.2063471)
- **OCM Relevance**:
  - Replace current BFS with hybrid traversal and work-stealing for large graphs.
  - Requires low-level concurrency control (e.g., lock-free structures).

---

## 3. Cycle Detection (Tarjan’s Algorithm)
- **Key Idea**: Detect strongly connected components (SCCs) to identify cycles.
- **Papers/Tools**:
  - [Tarjan’s Algorithm for SCCs (Math StackExchange)](https://math.stackexchange.com/questions/917414/tarjans-algorithm-to-determine-wheter-a-directed-graph-has-a-cycle)
  - [Dependency Graph Analyzer (GitHub)](https://github.com/Apollo87z/dependency-graph-analyzer)
- **OCM Relevance**:
  - Add cycle detection to avoid infinite loops (e.g., `A → B → A`).
  - Requires tracking lowlink and index for each node.

---

## 4. Resolver Ambiguity Resolution
- **Key Idea**: Use SAT-based dependency resolution (e.g., PubGrub) or trust scores to resolve conflicts.
- **Papers/Tools**:
  - [PubGrub: SAT-Based Dependency Resolution (Nesbitt, 2026)](https://nesbitt.io/2026/02/06/dependency-resolution-methods.html)
  - [uv’s PubGrub Resolver (Astral, 2026)](https://docs.astral.sh/uv/reference/internals/resolver/)
- **OCM Relevance**:
  - Replace "fail hard" approach with fallback resolvers or trust scores.
  - Requires modeling version constraints and incompatibilities.

---

## 5. Target Propagation & Pruning
- **Key Idea**: Use graph partitioning or community detection to prune targets.
- **Papers/Tools**:
  - [Graph Partitioning for HPC (ACM, 2000)](https://dl.acm.org/doi/10.1145/358438.349436)
- **OCM Relevance**:
  - Allow explicit target pruning (e.g., "only transfer to repo X").
  - Requires policy-based propagation (e.g., annotations on components).

---

## 6. Batched Resource Transfers
- **Key Idea**: Group resources by type (e.g., OCI artifacts) for bulk transfers.
- **Industry Examples**:
  - [OCI Distribution Spec](https://github.com/opencontainers/distribution-spec)
  - [Helm Registry](https://helm.sh/docs/topics/registries/)
- **OCM Relevance**:
  - Batch OCI/Helm resources to reduce network overhead.
  - Requires type-aware buffering in the transfer pipeline.

---

## Recommendations for OCM
| Optimization               | Priority | Effort | Impact | Notes                                                                                     |
|----------------------------|----------|--------|--------|-------------------------------------------------------------------------------------------|
| **Incremental Discovery**  | High     | Medium | High   | Cache descriptors; only traverse updated components.                                      |
| **Hybrid BFS Traversal**   | High     | High   | High   | Replace BFS with TopDown/BottomUp and work-stealing.                                      |
| **Cycle Detection**        | Medium   | Low    | High   | Add Tarjan’s algorithm to avoid infinite loops.                                           |
| **Resolver Fallback**      | Medium   | Medium | Medium | Use trust scores or PubGrub for ambiguity resolution.                                     |
| **Target Pruning**         | Low      | Medium | Low    | Allow explicit target pruning (e.g., annotations).                                        |
| **Batched Resources**      | Medium   | Low    | Medium | Group OCI/Helm resources for bulk transfers.                                              |