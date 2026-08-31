---
title: "Send Diki findings to ODG-API"
description: "Required requests to create Diki findings as GitHub issues via delivery-service."
weight: 5
toc: true
---

## Goal

Push [Diki](https://github.com/gardener/diki) compliance findings to the delivery-database and create the required runtime artefact resource in the ODG cluster so that the issue-replicator generates GitHub issues for them.

## You'll end up with

- Diki findings stored in the ODG delivery-database
- A `RuntimeArtefact` CR in the ODG Kubernetes cluster
- GitHub issues created by the issue-replicator for each Diki rule finding

**Estimated time:** ~10 minutes

## Prerequisites

- A running ODG instance (see [Deploying the Open Delivery Gear Locally]({{< relref "docs/how-to/odg/deploying-the-open-delivery-gear-locally/" >}}))
- Access to the delivery-service REST API (see [REST API documentation](https://github.com/open-component-model/delivery-service?tab=readme-ov-file#rest-api-documentation))
- `curl` or equivalent HTTP client

## Steps

### Upload Diki findings

Send a `PUT` request to `/artefacts/metadata`. The body must contain a list of entries; one entry must be of type `meta/artefact_scan_info`. Diki findings are separated by rule using the `finding/diki` type:

```json
{
  "entries": [
    {
      "artefact": {
        "component_name": "<component_name>",
        "component_version": "<component_version>",
        "artefact_kind": "runtime",
        "artefact": {
          "artefact_name": "<artefact_name>",
          "artefact_version": "diki",
          "artefact_type": "dikiReport"
        }
      },
      "meta": {
        "type": "meta/artefact_scan_info",
        "datasource": "diki"
      },
      "data": {}
    },
    {
      "artefact": {
        "component_name": "<component_name>",
        "component_version": "<component_version>",
        "artefact_kind": "runtime",
        "artefact": {
          "artefact_name": "<artefact_name>",
          "artefact_version": "diki",
          "artefact_type": "dikiReport"
        }
      },
      "meta": {
        "type": "finding/diki",
        "datasource": "diki"
      },
      "discovery_date": "<YYYY-MM-DD>",
      "data": {
        "severity": "<severity>",
        "provider_id": "<provider_id>",
        "ruleset_id": "<ruleset_id>",
        "ruleset_version": "<ruleset_version>",
        "rule_id": "<rule_id>",
        "checks": [
          {
            "message": "<message>",
            "targets": {}
          }
        ]
      }
    }
  ]
}
```

Repeat the `finding/diki` entry for each Diki rule finding.

### Register the runtime artefact

Send a `PUT` request to `/service-extensions/runtime-artefacts` to create the `RuntimeArtefact` CR in the cluster:

```json
{
  "artefacts": [
    {
      "component_name": "<component_name>",
      "component_version": "<component_version>",
      "artefact_kind": "runtime",
      "artefact": {
        "artefact_name": "<artefact_name>",
        "artefact_version": "diki",
        "artefact_type": "dikiReport"
      }
    }
  ]
}
```

### Clean up old findings (optional)

To remove stale Diki findings:

1. Send a `DELETE` request to `/artefacts/metadata` with the entries to remove.
2. Send a `DELETE` request to `/service-extensions/runtime-artefacts?name=<name>` to remove the runtime artefact CR.

## Troubleshooting

### Symptom: GitHub issues are not created after upload

**Cause:** The `RuntimeArtefact` CR was not created, so the issue-replicator does not process the artefact.

**Fix:** Verify the `PUT /service-extensions/runtime-artefacts` request succeeded and the CR exists in the `delivery` namespace.

### Symptom: Stale findings keep appearing in the dashboard

**Cause:** Old entries were not deleted after re-upload.

**Fix:** Use `DELETE /artefacts/metadata` to remove entries that are no longer current before or after uploading new ones.

## Next steps

- [Diagnose failed SBOM generation]({{< relref "docs/how-to/odg/diagnose-failed-sbom-generation/" >}})
- [Get all vulnerabilities for a component]({{< relref "docs/how-to/odg/get-all-vulnerabilities-for-a-component/" >}})

## Related documentation

- [Lifecycling GitHub Issues]({{< relref "docs/concepts/odg/lifecycling-github-issues/" >}})
- [Reacting upon OCM events]({{< relref "docs/concepts/odg/reacting-upon-ocm-events/" >}})
- [Correlating metadata to OCM]({{< relref "docs/concepts/odg/correlating-metadata-to-ocm/" >}})
