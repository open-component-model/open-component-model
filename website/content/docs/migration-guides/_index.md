---
title: "Migration Guides"
description: "Move existing OCM configurations off deprecated or removed features."
icon: "🔄"
weight: 55
sidebar:
  collapsed: true
---

Migration guides help you update existing OCM setups when a configuration format, flag, or behavior is deprecated or removed.
Each guide names what changed, shows the before and after configuration, and explains how to verify the result.

## Available Guides

- [Migrate Legacy Credentials]({{< relref "docs/migration-guides/legacy-credential-compatibility.md" >}}) — update a legacy `.ocmconfig` to modern field names and optional typed credentials.
- [Migrate from Fallback to Deterministic Repository Resolvers]({{< relref "docs/migration-guides/migrate-from-deprecated-resolvers.md" >}}) — replace deprecated fallback resolvers with glob-based resolvers.
- [Migrate from --upload-as to Uploader Configurations]({{< relref "docs/migration-guides/migrate-from-upload-as.md" >}}) — replace `--upload-as` and `uploadType` with the OCI uploader configuration.

For task-oriented guides on current features, see the [How-to Guides]({{< relref "docs/how-to/_index.md" >}}).
