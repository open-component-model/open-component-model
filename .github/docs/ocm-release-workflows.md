# OCM Release Process: Architecture and Design

This document explains the OCM CLI release workflow architecture, design decisions, and the two-phase release model (Release Candidate → Final Promotion).

---

## Overview

The release process uses a **two-phase model**:

1. **Release Candidate (RC)**: Creates RC tag, builds artifacts, generates attestations, publishes pre-release
2. **Final Promotion**: Verifies RC attestations, creates final tag from RC commit, promotes OCI tags, publishes final release

Both phases run through `cli-release.yml` controlled by the `release_candidate` input parameter.

---

## Core Concepts

### Release Candidate (RC)
- Versioned as `cli/v0.X.Y-rc.N` (e.g., `cli/v0.8.0-rc.1`)
- Built from release branch (`releases/v0.X`)
- Full build + attestation generation
- Published as GitHub **pre-release**
- RC commit is immutable source of truth for final promotion

### Final Promotion
- Uses **existing RC** as source
- Verifies all RC attestations before proceeding
- Creates final tag (`cli/v0.X.Y`) from **same commit** as RC
- Promotes OCI tags (`:rc.N` → `:0.X.Y` + `:latest`)
- Publishes GitHub **final release** with RC assets

### Attestations
Supply-chain security artifacts proving build provenance:
- One bundle per binary (e.g., `attestation-ocm-linux-amd64.jsonl`)
- One bundle for OCI image (`attestation-ocm-oci-image.jsonl`)
- Index file (`attestations-index.json`) mapping subjects to bundles

---

## Architecture

### Workflow Files

```
release-branch.yml          Creates releases/v0.X branches
    │
    └─> cli-release.yml     Orchestrates RC and Final paths
            │
            ├─> release-candidate-version.yml    Metadata computation
            └─> cli.yml                          Build + Publish
```

### Job Flow

#### RC Path (`release_candidate=true`)
```
prepare → tag_rc → build → release_rc
   │         │        │         │
   │         │        │         └─> Export attestations
   │         │        └─> Attest binaries + OCI image
   │         └─> Create annotated RC tag
   └─> Compute version, generate changelog
```

#### Final Path (`release_candidate=false`)
```
prepare → validate_final → verify_attestations → tag_final → promote_image → release_final
   │            │                  │                  │             │              │
   │            │                  │                  │             │              └─> Publish final release
   │            │                  │                  │             └─> ORAS tag promotion
   │            │                  │                  └─> Create final tag from RC commit
   │            │                  └─> Verify all RC attestations
   │            └─> Ensure RC exists
   └─> Resolve latest RC metadata
```

---

## Key Design Decisions

### 1. Immutable RC Commits
**Why**: Final tag references the **exact commit** of the RC tag, ensuring binary reproducibility.

```javascript
// tag_final job
const rcSha = execSync(`git rev-parse "refs/tags/${rcTag}^{commit}"`);
execSync(`git tag -a "${finalTag}" "${rcSha}" -m "Promote ${rcTag}"`);
```

If the final tag already exists, the workflow aborts—tags are immutable.

### 2. Digest-Based OCI Verification
**Why**: OCI tags are mutable and can be overwritten between RC creation and final promotion.

The attestation index stores the image digest:
```json
{
  "image": {
    "ref": "ghcr.io/owner/cli:0.8.0-rc.1",
    "digest": "sha256:abc123..."
  }
}
```

Verification uses `oci://repo@sha256:...` (by digest), not `oci://repo:tag` (by tag).

### 3. Human-Readable Attestation Names
**Why**: Easier debugging and asset management.

Instead of cryptic hash names:
```
sha256:def456...7890ab.jsonl
```

We use:
```
attestation-ocm-linux-amd64.jsonl
attestation-ocm-oci-image.jsonl
```

The index maps subjects to bundles:
```json
{
  "attestations": [
    {
      "subject": "ocm-linux-amd64",
      "digest": "sha256:def456...",
      "bundle": "attestation-ocm-linux-amd64.jsonl"
    }
  ]
}
```

### 4. No Re-computation for Final
**Why**: Avoids drift between RC and Final. The final release notes are copied from RC, not regenerated via `git-cliff`.

```bash
# release_final job
gh release view "$RC_TAG" --json body --jq '.body' > RELEASE_NOTES.md
```

### 5. Separate Metadata Workflow
**Why**: Centralized version computation logic, reusable for RC and Final.

`release-candidate-version.yml` outputs:
- **RC mode**: `new_tag`, `new_version`, `changelog_b64`
- **Final mode**: `latest_rc_tag`, `latest_promotion_tag`

---

## Attestation System

### Export (RC Release)
**Script**: `export-attestations.js`

**Inputs**: Build artifacts directory, `IMAGE_DIGEST` from build output

**Process**:
1. Download attestations from GitHub for each binary via `gh attestation download <file>`
2. Download OCI image attestation via `gh attestation download oci://repo@<digest>`
3. Rename bundles to human-readable names
4. Generate `attestations-index.json`

**Key Detail**: Uses `IMAGE_DIGEST` **directly from build output**, no registry lookup needed.

### Verify (Final Promotion)
**Script**: `verify-attestations.js`

**Inputs**: RC release assets directory (includes attestation bundles + index)

**Process**:
1. Load `attestations-index.json`
2. For each binary: `gh attestation verify <file> --bundle <bundle>`
3. For OCI image: `gh attestation verify oci://repo@<digest> --bundle <bundle>`

**Key Detail**: Verifies by **digest from index**, ensuring exact RC image, regardless of tag mutations.

### Attestations Index Format
```json
{
  "version": "1",
  "generated_at": "2026-02-13T09:27:00.000Z",
  "rc_version": "0.8.0-rc.1",
  "image": {
    "ref": "ghcr.io/owner/cli:0.8.0-rc.1",
    "digest": "sha256:abc123..."
  },
  "attestations": [
    {
      "subject": "ocm-linux-amd64",
      "type": "binary",
      "digest": "sha256:def456...",
      "bundle": "attestation-ocm-linux-amd64.jsonl"
    },
    {
      "subject": "ghcr.io/owner/cli:0.8.0-rc.1",
      "type": "oci-image",
      "digest": "sha256:abc123...",
      "bundle": "attestation-ocm-oci-image.jsonl"
    }
  ]
}
```

---

## Extension Points

### Multi-Component Support
The attestation scripts are generic and accept `COMPONENT_PATH`:
- `COMPONENT_PATH=cli` → CLI Release
- `COMPONENT_PATH=kubernetes/controller` → Controller Release (planned)

### Additional Artifacts
To attest more artifacts (e.g., Helm charts):
1. Extend `ASSET_PATTERNS`: `["bin/ocm-*", "helm/*.tgz"]`
2. New entries automatically included in index
3. Verification handles all entries from index

---

## Validation

```bash
# Run script tests
node .github/scripts/compute-rc-version.test.js
node .github/scripts/resolve-latest-rc.test.js
node .github/scripts/export-attestations.test.js
node .github/scripts/verify-attestations.test.js

# Dry-run validation (if release flow changed)
# Trigger cli-release.yml with dry_run=true for both RC and Final
```

---

## Summary

The OCM release process ensures:
- **Immutability**: Final releases reference exact RC commits
- **Security**: Full attestation chain for supply-chain verification
- **Reproducibility**: Digest-based verification prevents tag manipulation
- **Extensibility**: Generic scripts support multi-component releases
- **Safety**: Multiple validation gates before final promotion