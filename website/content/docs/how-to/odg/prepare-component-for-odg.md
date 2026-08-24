---
title: "Prepare Your Component for ODG"
description: "This guide is for component authors who want to get the most out of ODG scanning. By adding a small set of OCM labels to your component descriptor, you can…"
weight: 100
toc: true
---
This guide is for component authors who want to get the most out of ODG
scanning. By adding a small set of [OCM labels]({{< relref "/docs/reference/odg/ocm-labels.md" >}})
to your component descriptor, you can control scan behaviour, ensure findings
are routed to the right team, and provide context that helps ODG produce more
accurate results.

## Prerequisites

- An OCM component descriptor (`component-descriptor.yaml` or equivalent)
- Familiarity with the [OCM label format](https://ocm.software/docs/reference/component-descriptor/#component-labels)

## Declare Responsible Owners

Add the `odg.ocm.software/responsibles` label so that ODG and the issue
replicator know whom to assign findings to.

```yaml
labels:
  - name: odg.ocm.software/responsibles
    version: v1
    value:
      - type: githubTeam
        teamname: my-org/my-team
```

See the [label reference]({{< relref "/docs/reference/odg/ocm-labels.md" >}}#odgocmsoftwareresponsibles-v1)
for all supported types (`githubUser`, `codeowners`, etc.).

{{< callout "note" >}}
The responsibles extension can override or extend these assignments at runtime
via configurable rules. See
[Responsibles]({{< relref "/docs/concepts/odg/responsibles.md" >}}) for details.
{{< /callout >}}

## Provide Risk Profile Context

Add the `security.ocm.software/risk-profile` label to describe the deployment
context of your component. ODG uses this to suggest adjusted CVE severity
scores that reflect actual exposure rather than the theoretical maximum.

```yaml
labels:
  - name: security.ocm.software/risk-profile
    version: v1
    value:
      network_exposure: "private"
      authentication_enforced: true
      user_interaction: "end-user"
      confidentiality_requirement: "low"
      integrity_requirement: "high"
      availability_requirement: "high"
```

Only set the fields that are meaningful for your component; omitted fields are
treated as unknown and do not affect rescoring. See the
[label reference]({{< relref "/docs/reference/odg/ocm-labels.md" >}}#securityocmsoftwarerisk-profile-v1)
for all fields and allowed values.

## Skip Binary or Source Scans

You can configure whether ODG should run binary vulnerability scans or SAST
(Static Application Security Testing) source analysis. Usually `skip` is set
when the pipeline already ran the equivalent scan.

```yaml
labels:
  - name: odg.ocm.software/binary-scan-policy
    version: v1
    value:
      policy: "skip"
      comment: "Scanned upstream, results attached as SBOM"
  - name: odg.ocm.software/source-scan-policy
    version: v1
    value:
      policy: "skip"
      comment: "We use gosec for SAST scanning, see attached log"
```

See the
[label reference]({{< relref "/docs/reference/odg/ocm-labels.md" >}}#odgocmsoftwarebinary-scan-policy-v1)
for all fields and allowed values.
