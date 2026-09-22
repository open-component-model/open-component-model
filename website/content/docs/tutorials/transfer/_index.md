---
title: "Transfer"
description: "Tutorials for configuring resolvers and credentials across transfer environments."
weight: 30
sidebar:
  collapsed: false
---

During the transfer phase you move component versions across registries and environments. These
tutorials cover the configuration side of transfer: how OCM resolves components across multiple
registries and how it picks the right credentials for each target.

- [Working with Resolvers]({{< relref "docs/tutorials/transfer/configure-resolvers.md" >}}) — configure glob-based resolver rules to locate components across a registry hierarchy
- [Understand Credential Resolution]({{< relref "docs/tutorials/transfer/credential-resolution.md" >}}) — experiment with the credential lookup chain to understand how OCM selects credentials for a target registry
