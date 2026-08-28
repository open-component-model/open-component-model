---
title: "Get all vulnerabilities for a Component"
description: "Query all identified vulnerabilities within a Component using ODG API."
weight: 4
toc: true
---

## Goal

Query all identified vulnerabilities within a Component using ODG API.

## You'll end up with

- A list of CVEs
- Metadata like initial discovery date and datasources
- Package information where the CVEs have been detected in

**Estimated time:** ~5 minutes

## Prerequisites

- An ODG instance
- A shell (like `bash` or `zsh`)
- `cURL`, `awk`, `jq`
- A GitHub token with read access to the ODG instance

## Steps

### Prepare the environment

```bash
export ODG_API='https://delivery-service.demo.ci.gardener.cloud'
export GH_TOKEN='github_pat_xxx'
export GH_API='https://api.github.com'
```

### Authenticate against the ODG instance

```bash
export ODG_TOKEN=$(curl -c - "${ODG_API}/auth?api_url=${GH_API}&access_token=${GH_TOKEN}" | awk '/bearer_token/ {print $NF}')
```

### Fetch vulnerabilities from the API

```bash
curl -X POST -d '{"entries": [{"component_name": "acme.org/sovereign/postgres", "component_version": "1.0.0"}]}' -H "Accept: application/json" -H "Authorization: Bearer ${ODG_TOKEN}" "${ODG_API}/artefacts/metadata/query?type=finding/vulnerability" | jq .
```

## Troubleshooting

### Symptom: Empty response or authentication error

**Cause:** The GitHub token does not have read access to the ODG instance, or the token has expired.

**Fix:** Verify your token permissions and re-export `GH_TOKEN` before re-running the authentication step.

## Next steps

- [Run custom SQL commands in ODG]({{< relref "docs/how-to/odg/run-custom-sql-commands-in-odg/" >}})
- [Change the SLAs for vulnerability findings]({{< relref "docs/how-to/odg/change-the-slas-for-vulnerability-findings/" >}})

## Related documentation

- [Correlating metadata to OCM]({{< relref "docs/concepts/odg/correlating-metadata-to-ocm/" >}})
- [SLA violation profiler]({{< relref "docs/concepts/odg/sla-violation-profiler/" >}})
