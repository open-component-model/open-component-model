---
title: "Get all vulnerabilities for a Component"
description: "Query all identified vulnerabilities within a Component using ODG API."
weight: 4
toc: true
---

## Goal

Query all identified vulnerabilities within a Component using ODG API.

## You will end up with

- a list of CVEs
- metadata like initial discovery date and datasources
- package information where the CVEs have been detected in

## Prerequisites

- An ODG instance
- a shell (like `bash` or `zsh`)
- `cURL`, `awk`, `jq`
- A GitHub token privileged to read-access the ODG instance

## Actions

### Preparing Environment

```bash
export ODG_API='https://delivery-service.demo.ci.gardener.cloud'
export GH_TOKEN='github_pat_xxx'
export GH_API='https://api.github.com'
```

### Authenticate against ODG instance

```bash
export ODG_TOKEN=$(curl -c - "${ODG_API}/auth?api_url=${GH_API}&access_token=${GH_TOKEN}" | awk '/bearer_token/ {print $NF}')
```

### Fetch Vulnerabilities from API

```bash
curl -X POST -d '{"entries": [{"component_name": "acme.org/sovereign/postgres", "component_version": "1.0.0"}]}' -H "Accept: application/json" -H "Authorization: Bearer ${ODG_TOKEN}" "${ODG_API}/artefacts/metadata/query?type=finding/vulnerability" | jq .
```
