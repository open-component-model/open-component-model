---
title: "Artefact Metadata Search"
description: "API reference for searching artefact metadata stored in the delivery-db."
weight: 1
toc: true
---

The endpoint `POST /artefacts/metadata/query/by-search-expression` provides a structured query
interface for searching artefact metadata stored in the delivery-db. It supports filtering,
excluding, full-text search, OCM scope resolution, severity comparisons, and cursor-based
pagination.

A companion endpoint `GET /artefacts/metadata/query-attributes` returns the list of queryable
fields, their types, and supported operators — useful for building UIs or validating queries
client-side.

---

## Request format

```javascript
{
  "criteria": [ /* criterion objects */ ],
  "limit": 50,
  "sort": [ { "field": "meta.creation_date", "order": "desc" } ],
  "cursor": null
}
```

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `criteria` | array | `[]` | List of filter criteria (see below) |
| `limit` | integer | `50` | Page size, capped server-side at `200` |
| `sort` | array | `[{"field": "meta.creation_date", "order": "desc" }, {"field": "id", "order": "desc" }]` | Sort specification (see [Sorting](#sorting)) |
| `cursor` | object | `null` | Seek cursor for next page (see [Pagination](#pagination)) |

---

## Criteria

Every criterion is a JSON object with a required `type` field. All criteria are **AND**ed
together. Within criteria of the same type and attribute, **OR** semantics apply (see details
per type below).

An optional `"mode": "exclude"` field negates any criterion.

### 1. `ocm` — component/artefact scope

Filters by OCM component identity (`name` or `name:version`).

```json
{ "type": "ocm", "value": "acme.org/my-comp" }
{ "type": "ocm", "value": "acme.org/my-comp:1.2.3" }
{ "type": "ocm", "value": "acme.org/my-comp:1.2.3", "recursive": true }
{ "type": "ocm", "value": "acme.org/my-comp:1.2.3", "mode": "exclude" }
```

| Field | Required | Description |
| --- | --- | --- |
| `value` | yes | `name` or `name:version` |
| `recursive` | no | If `true`, resolves the full component dependency tree and matches all transitive components. Requires a versioned value. |
| `mode` | no | `"exclude"` negates the predicate |

**Multiple** `ocm` **includes are OR-ed** — useful for scoping across several components at once.

**Version-less queries** (`acme.org/my-comp` without a version) match all rows with that
component name regardless of version, including rows where `component_version` is `NULL`.

**Versioned queries** without a component descriptor lookup perform a strict
`component_name = X AND component_version = Y` match. With a component descriptor lookup
configured, the server resolves the component's artefact list from the OCM registry and
additionally matches rows where `component_version` is `NULL` but the artefact key matches
a known artefact of that component version (handles findings stored without a component
version).

**Rescoring-aware version matching**: when `type:rescorings` is present in the criteria,
versioned `ocm:` filters also match rows where `component_version IS NULL` (in addition to
the exact version). This is because rescorings are typically stored without a component
version since they apply across all versions of a component.

---

### 2. `artefact-metadata` — field filter

Filters on a specific attribute of the artefact metadata row.

```json
{ "type": "artefact-metadata", "attr": "type", "op": "eq", "value": "finding/vulnerability" }
{ "type": "artefact-metadata", "attr": "data.cve", "op": "in", "values": ["CVE-2024-1234", "CVE-2024-5678"] }
{ "type": "artefact-metadata", "attr": "data.severity", "op": "cmp", "cmp": ">=", "value": "HIGH" }
{ "type": "artefact-metadata", "attr": "meta.creation_date", "op": "range", "gte": "2025-01-01T00:00:00Z" }
{ "type": "artefact-metadata", "attr": "type", "op": "eq", "value": "finding/vulnerability", "mode": "exclude" }
```

#### Supported `attr` values

| Attribute | Column / source | Notes |
| --- | --- | --- |
| `type` | `ArtefactMetaData.type` | See [Data types](#data-types) |
| `referenced_type` | `ArtefactMetaData.referenced_type` | Only populated for `rescorings` rows; holds the finding type the rescoring applies to |
| `datasource` | `ArtefactMetaData.datasource` | |
| `artefact_kind` | `ArtefactMetaData.artefact_kind` | |
| `artefact.name` | `ArtefactMetaData.artefact_name` | |
| `artefact.version` | `ArtefactMetaData.artefact_version` | |
| `artefact.type` | `ArtefactMetaData.artefact_type` | |
| `meta.<key>` | JSONB `meta` column, key extracted as text | e.g. `meta.creation_date`, `meta.last_update` |
| `data.<path>` | JSONB `data` column, nested path extracted as text | e.g. `data.cve`, `data.severity`, `data.package_name`, `data.osid.NAME` |

#### Supported `op` values

| `op` | Required fields | Semantics |
| --- | --- | --- |
| `eq` | `value` | Exact match. If `value` contains `*`, performs a case-insensitive `LIKE` match (`*` → `%`). |
| `in` | `values` (list) | Any-of match (OR semantics). All values compared as strings. |
| `range` | `gte` and/or `lte` | Datetime range, both bounds inclusive. Values must be ISO 8601. Either bound may be omitted for an open range. Only applicable to datetime attributes. |
| `cmp` | `cmp`, `value` | Comparison operator: `>`, `>=`, `<`, `<=`, `==`, `!=`. For `data.severity`, symbolic severity IDs (e.g. `HIGH`) are resolved to numeric values when exactly one finding type is selected (see [Severity comparisons](#severity-comparisons)). |

#### AND/OR semantics

- Criteria on **different attributes** are **AND**ed.
- Multiple criteria on the **same attribute** (same `attr`) are **OR**ed (include) or

  `NOT(OR(...))` (exclude).

---

### 3. `fulltext` — free-text search

Searches for a token across a set of default fields using a case-insensitive contains match.

```json
{ "type": "fulltext", "value": "kerberos" }
{ "type": "fulltext", "value": "kerberos", "fields": ["data.summary", "data.cve"] }
{ "type": "fulltext", "value": "kerberos", "mode": "exclude" }
```

| Field | Required | Description |
| --- | --- | --- |
| `value` | yes | Token to search for |
| `fields` | no | List of attributes to search in. Defaults to `data.summary`, `data.cve`, `data.package_name`, `data.package_version`, `artefact.name`, `ocm.name` |
| `mode` | no | `"exclude"` negates the predicate |

Multiple fulltext criteria are **AND**ed — all tokens must match (somewhere across the
configured fields).

---

## Severity comparisons

Severity values in the DB are stored as string IDs (e.g. `NONE`, `LOW`, `MEDIUM`, `HIGH`,
`CRITICAL`). Numeric comparison (`op: cmp`) works directly against the raw string. However,
for symbolic comparisons (`data.severity>=HIGH`) to work correctly, the server resolves the
symbolic ID to a numeric value via the configured finding categorisations.

**This resolution requires exactly one included finding type in the criteria.** If multiple
finding types are selected (or none), symbolic severity comparisons will be rejected with a
`400 Bad Request`.

Example:

```json
[
  { "type": "artefact-metadata", "attr": "type", "op": "eq", "value": "finding/vulnerability" },
  { "type": "artefact-metadata", "attr": "data.severity", "op": "cmp", "cmp": ">=", "value": "HIGH" }
]
```

---

## Data types

The `type` field on each row identifies what kind of data it holds. Common values:

| Value | Description |
| --- | --- |
| `finding/vulnerability` | CVE vulnerability finding |
| `finding/license` | License compliance finding |
| `finding/crypto` | Cryptography finding |
| `finding/malware` | Malware scan finding |
| `finding/diki` | Diki policy finding |
| `finding/sast` | SAST finding |
| `finding/falco` | Falco runtime finding |
| `finding/osid` | OS identification finding |
| `finding/ip` | IP finding |
| `rescorings` | Custom rescoring applied to a finding |
| `meta/artefact_scan_info` | Scan metadata |
| `compliance/snapshots` | Compliance snapshot |

For `rescorings` rows, `referenced_type` holds the finding type the rescoring applies to
(e.g. `finding/vulnerability`).

---

## Querying rescorings

Rescorings might be stored without a component version because it can be intended to apply across
all versions of a component. To query rescorings effectively:

```json
[
  { "type": "artefact-metadata", "attr": "type", "op": "eq", "value": "rescorings" }
]
```

Scope to a specific finding type:

```json
[
  { "type": "artefact-metadata", "attr": "type",            "op": "eq", "value": "rescorings" },
  { "type": "artefact-metadata", "attr": "referenced_type", "op": "eq", "value": "finding/vulnerability" }
]
```

Scope to a component (versioned). Because rescorings typically have no `component_version`,
the server automatically widens the match to also include rows where `component_version IS NULL`
whenever `type: rescorings` is the only included type filter:

```json
[
  { "type": "artefact-metadata", "attr": "type", "op": "eq", "value": "rescorings" },
  { "type": "ocm", "value": "acme.org/my-comp:1.2.3" }
]
```

This returns rescorings for `acme.org/my-comp` regardless of whether they were stored with
version `1.2.3` or without any version.

---

## Sorting

Default sort is `meta.creation_date DESC, id DESC`. A secondary `id` sort is always appended
automatically to guarantee a stable cursor position.

Supported sort fields:

| Field | Description |
| --- | --- |
| `meta.creation_date` | Creation timestamp (datetime-aware) |
| `meta.last_update` | Last update timestamp (datetime-aware) |
| `type` | Data type string |
| `ocm.name` | Component name |
| `ocm.version` | Component version |
| `id` | Row ID (always appended as tiebreaker) |

All sort fields in a single request must use the same direction (`asc` or `desc`). Mixed
sort orders are rejected with `400 Bad Request`.

---

## Pagination

The endpoint uses **cursor-based client-facing pagination**. After receiving a page,
pass the returned `nextCursor` object as `cursor` in the next request to continue from where
the previous page ended. A `null` `nextCursor` means there are no more results.

The cursor encodes the sort-field values of the last returned row. It is opaque and must not
be modified. Changing `criteria` or `sort` between pages yields undefined results.

To avoid pagination becoming arbitrarily expensive on very large result sets that are
heavily filtered post-fetch (by finding configs), the server over-fetches from the database
(up to `10x` the page size per round, capped at 2000 rows) and iterates up to 10 rounds
before returning a partial page.

---

## Examples

### All vulnerability findings for a component version

```json
{
  "criteria": [
    { "type": "artefact-metadata", "attr": "type", "op": "eq", "value": "finding/vulnerability" },
    { "type": "ocm", "value": "acme.org/my-comp:1.2.3" }
  ]
}
```

### High or critical vulnerabilities across a component tree

```json
{
  "criteria": [
    { "type": "artefact-metadata", "attr": "type",          "op": "eq",  "value": "finding/vulnerability" },
    { "type": "artefact-metadata", "attr": "data.severity", "op": "cmp", "cmp": ">=", "value": "HIGH" },
    { "type": "ocm", "value": "acme.org/my-comp:1.2.3", "recursive": true }
  ]
}
```

### Specific CVE across all component versions

```json
{
  "criteria": [
    { "type": "artefact-metadata", "attr": "type",     "op": "eq", "value": "finding/vulnerability" },
    { "type": "artefact-metadata", "attr": "data.cve", "op": "eq", "value": "CVE-2024-1234" }
  ]
}
```

### All rescorings for a component, any finding type

```json
{
  "criteria": [
    { "type": "artefact-metadata", "attr": "type", "op": "eq", "value": "rescorings" },
    { "type": "ocm", "value": "acme.org/my-comp:1.2.3" }
  ]
}
```

### Findings created after a specific date, excluding a known noisy component

```json
{
  "criteria": [
    { "type": "artefact-metadata", "attr": "type",               "op": "eq",    "value": "finding/vulnerability" },
    { "type": "artefact-metadata", "attr": "meta.creation_date", "op": "range", "gte": "2025-06-01T00:00:00Z" },
    { "type": "ocm", "value": "acme.org/noisy-comp", "mode": "exclude" }
  ]
}
```

### Free-text search for a package name

```json
{
  "criteria": [
    { "type": "artefact-metadata", "attr": "type", "op": "eq", "value": "finding/vulnerability" },
    { "type": "fulltext", "value": "openssl" }
  ]
}
```
