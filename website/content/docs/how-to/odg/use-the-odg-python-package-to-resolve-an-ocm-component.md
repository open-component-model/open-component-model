---
title: "Use the ODG Python Package to resolve an OCM component"
description: "Resolve an OCM component using the ODG API and the ODG Python Package."
weight: 3
toc: true
---

## Goal

Resolve an OCM component using the ODG API and the ODG Python Package.

## You'll end up with

- A Python script that calls the ODG API to resolve an OCM component

**Estimated time:** ~5 minutes

## Prerequisites

- An ODG instance
- `python3`, `pip3`
- A GitHub token with read access to the ODG instance

## Steps

### Install the package

The package [is published to pypi.org](https://pypi.org/project/odg-client/) and can be installed with `pip3`.

```shell
pip3 install odg-client
```

{{< callout type="tip" >}}
If you are not using virtual environments, you have to additionally provide the `--break-system-packages` flag.
{{< /callout >}}

### Initialise the client

```python
import odg_client

ODG_API = 'https://delivery-service.demo.ci.gardener.cloud'
GH_TOKEN = 'github_pat_xxx'
GH_API = 'https://api.github.com'

odg_api = odg_client.DeliveryServiceClient(
    routes=odg_client.DeliveryServiceRoutes(
        base_url=ODG_API,
    ),
    auth_token=GH_TOKEN,
    api_url=GH_API,
)
```

### Resolve an OCM component

The `odg-client` features functions wrapping ODG-API endpoints for convenience.
There is no guarantee that all endpoints are covered, but the low-level client functions can be used to fill the gap, as they implement authentication and the overall request flow.

An OCM component is resolved via `GET /ocm/component` endpoints, which features parameters like `component-name` and `ocm-repo`.
The following Python call resolves the OCM component `acme.org/sovereign/postgres` at a specific version:

```python
component_descriptor = odg_api.component_descriptor(
    name='acme.org/sovereign/postgres',
    version='1.0.0'
)
```

## Troubleshooting

### Symptom: `pip3 install` fails in a managed Python environment

**Cause:** System Python environment rejects installs without explicit consent.

**Fix:** Add `--break-system-packages` flag to the install command.

## Next steps

- [Get all vulnerabilities for a component]({{< relref "docs/how-to/odg/get-all-vulnerabilities-for-a-component/" >}})

## Related documentation

- [Correlating metadata to OCM]({{< relref "docs/concepts/odg/correlating-metadata-to-ocm/" >}})
