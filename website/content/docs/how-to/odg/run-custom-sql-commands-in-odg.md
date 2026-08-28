---
title: "Run custom SQL commands in ODG"
description: "Establish a psql connection to the ODG database for executing custom SQL queries."
weight: 9
toc: true
---

## Goal

Establish a `psql` connection to the ODG database for executing custom SQL queries.

## You'll end up with

- An interactive `psql` shell connected to the ODG database
- The ability to execute arbitrary SQL commands against ODG

**Estimated time:** ~5 minutes

## Prerequisites

- A running ODG instance
- `kubectl` CLI tool
- A kubeconfig privileged to exec into the ODG database pod (`create` on `pods/exec` subresource)

## Steps

### Spawn a shell in the database pod

First, connect to the pod running the PostgreSQL database:

```bash
kubectl exec --namespace <odg-namespace> -it delivery-db-0 -- sh
```

This spawns a shell in the pod using the Kubernetes API and container runtime, creating an interactive session that streams:

- Your terminal's STDIN to the container's process
- The container's STDOUT/STDERR to your terminal

### Connect to the database using `psql`

The `psql` client is installed in the container and available on the PATH. Establish a connection with:

```bash
psql -U postgres
```

The password is already configured via environment variables in the container, so you do **not** need to specify it.

You now have an active `psql` session. Explore the database schema using commands such as `\dt`:

```sql
               List of relations
 Schema |       Name        | Type  |  Owner
--------+-------------------+-------+----------
 public | artefact_metadata | table | postgres
 public | blob_store        | table | postgres
 public | cache             | table | postgres
 public | user_identifiers  | table | postgres
 public | users             | table | postgres
(5 rows)
```

### Query vulnerabilities for a component

Use the `\x` command to enable extended `psql` display mode for improved readability of query results in the terminal.

Query the most recent vulnerability entry for a component:

```sql
SELECT * FROM artefact_metadata
WHERE TYPE = 'finding/vulnerability'
AND component_name = 'acme.org/sovereign/postgres'
ORDER BY creation_date DESC LIMIT 1;
```

This returns the raw low-level database entry. To extract the CVE identifier specifically, query the `data` field:

```sql
SELECT data->>'cve' AS CVE FROM artefact_metadata
WHERE TYPE = 'finding/vulnerability'
AND component_name = 'acme.org/sovereign/postgres'
ORDER BY creation_date DESC LIMIT 1;
```

This produces a more focused output, for example:

```sql
-[ RECORD 1 ]-------
cve | CVE-2026-42504
```

### Search for a CVE across multiple components

To reverse the previous query and find all components affected by a specific CVE:

```sql
SELECT component_name FROM artefact_metadata
WHERE TYPE = 'finding/vulnerability'
AND data->>'cve' = 'CVE-2026-42504';
```

This produces output such as:

```sql
-[ RECORD 1 ]--+---------------------------------------
component_name | opendesk.poc.sap.com/jitsi
-[ RECORD 2 ]--+---------------------------------------
component_name | acme.org/sovereign/postgres
-[ RECORD 3 ]--+---------------------------------------
component_name | opendesk.poc.sap.com/services-external
-[ RECORD 4 ]--+---------------------------------------
component_name | opendesk.poc.sap.com/jitsi
-[ RECORD 5 ]--+---------------------------------------
component_name | ocm.software/ocmcli
```

### More advanced queries

To effectively query ODG at the SQL level, you need to understand the underlying data model. See [Correlating metadata to OCM]({{< relref "docs/concepts/odg/correlating-metadata-to-ocm/" >}}) for details.

## Troubleshooting

### Symptom: `kubectl exec` is denied

**Cause:** The kubeconfig does not have `create` permission on the `pods/exec` subresource.

**Fix:** Request a kubeconfig with the appropriate RBAC permissions for the ODG namespace.

## Next steps

- [Change the SLAs for vulnerability findings]({{< relref "docs/how-to/odg/change-the-slas-for-vulnerability-findings/" >}})

## Related documentation

- [Correlating metadata to OCM]({{< relref "docs/concepts/odg/correlating-metadata-to-ocm/" >}})
