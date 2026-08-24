---
title: "Use the ODG Python Package to resolve an OCM component"
description: "Resolve an OCM component using the ODG API and the ODG Python Package."
weight: 30
toc: true
---
## Goal

Resolve an OCM component using the ODG API and the ODG Python Package.

## You will end up with

- a python script calling the ODG API to resolve an OCM component

## Prerequisites

- An ODG instance
- `python3`, `pip3`
- A GitHub token privileged to read-access the ODG instance

## Actions

### Installing the Package

The package [is published to pypi.org](https://pypi.org/project/odg-client/), thus can be installed with `pip3`.

```
pip3 install odg-client
```

**Hint**: if you are not using virtual environments, you have to additionally provide the `--break-system-packages` flag

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

### Resolve OCM component

The odg-client features functions wrapping ODG-API endpoints for convenience.
There is no guarantee that all endpoints are covered, but the low-level client functions could be used to fill the gap, as they implement authentication and the overall request flow.

An OCM component is resolved via `GET /ocm/component` endpoints, which features parameters like `component-name` and `ocm-repo`.
The corresponding python implementation to resolve the OCM component `acme.org/sovereign/postgres` in a certain version looks like this.

```python
component_descriptor = odg_api.component_descriptor(
    name='acme.org/sovereign/postgres',
    version='1.0.0'
)
```
