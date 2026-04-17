---
title: Repository
description: "API reference for the Repository custom resource (delivery.ocm.software/v1alpha1)"
weight: 2
toc: false
---

A **Repository** represents an OCM repository (for example, an OCI registry) that
contains component versions. The controller validates the repository at the
configured interval and propagates OCM configuration.

---

## API Specification

{{< schema-renderer url="/schemas/kubernetes/controller/delivery.ocm.software_repositories.yaml" >}}
