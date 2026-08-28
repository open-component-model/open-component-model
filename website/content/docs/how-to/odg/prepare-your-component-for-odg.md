---
title: "Prepare Your Component for ODG"
description: "Add OCM labels to control scan behaviour, routing, and accuracy in ODG."
weight: 10
toc: true
---

## Goal

Add OCM labels to your component descriptor to control scan behaviour, ensure findings are routed to the right team, and provide context that helps ODG produce more accurate results.

## You'll end up with

- A component descriptor with `odg.ocm.software/responsibles` so findings are routed to your team
- Optionally, a risk profile label for adjusted CVE severity scoring
- Optionally, scan-skip labels if your pipeline already runs equivalent scans

**Estimated time:** ~10 minutes

## Prerequisites

- An OCM component descriptor (`component-descriptor.yaml` or equivalent)
- Familiarity with the [OCM label format](https://ocm.software/docs/reference/component-descriptor/#component-labels)

## Steps

### Declare responsible owners

Add the `odg.ocm.software/responsibles` label so that ODG and the issue replicator know whom to assign findings to.

```yaml
labels:
  - name: odg.ocm.software/responsibles
    version: v1
    value:
      - type: githubTeam
        teamname: my-org/my-team
```

See the [label reference]({{< relref "docs/reference/odg/ocm-label-index/#odgocmsoftwareresponsibles-v1" >}})
for all supported types (`githubUser`, `codeowners`, etc.).

{{< callout type="note" >}}
The responsibles extension can override or extend these assignments at runtime via configurable rules. See
[Overwriting OCM component responsibles]({{< relref "docs/concepts/odg/overwriting-ocm-component-responsibles/" >}}) for details.
{{< /callout >}}

### Provide risk profile context

Add the `security.ocm.software/risk-profile` label to describe the deployment context of your component. ODG uses this to suggest adjusted CVE severity scores that reflect actual exposure rather than the theoretical maximum.

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

Only set the fields that are meaningful for your component; omitted fields are treated as unknown and do not affect rescoring. See the
[label reference]({{< relref "docs/reference/odg/ocm-label-index/#securityocmsoftwarerisk-profile-v1" >}})
for all fields and allowed values.

### Skip binary or source scans

You can configure whether ODG should run binary vulnerability scans or SAST (Static Application Security Testing) source analysis. Usually `skip` is set when the pipeline already ran the equivalent scan.

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
[label reference]({{< relref "docs/reference/odg/ocm-label-index/#odgocmsoftwarebinary-scan-policy-v1" >}})
for all fields and allowed values.

## Troubleshooting

### Symptom: Findings are not routed to the expected team

**Cause:** The `odg.ocm.software/responsibles` label is missing or has an unsupported type value.

**Fix:** Add the label with a supported type (`githubTeam`, `githubUser`, `codeowners`). If routing is still incorrect, check whether the responsibles extension is overriding assignments at runtime — see [Overwriting OCM component responsibles]({{< relref "docs/concepts/odg/overwriting-ocm-component-responsibles/" >}}).

## Next steps

- [Get all vulnerabilities for a component]({{< relref "docs/how-to/odg/get-all-vulnerabilities-for-a-component/" >}})
- [Change the SLAs for vulnerability findings]({{< relref "docs/how-to/odg/change-the-slas-for-vulnerability-findings/" >}})

## Related documentation

- [OCM label index]({{< relref "docs/reference/odg/ocm-label-index/" >}})
- [Overwriting OCM component responsibles]({{< relref "docs/concepts/odg/overwriting-ocm-component-responsibles/" >}})
- [Correlating metadata to OCM]({{< relref "docs/concepts/odg/correlating-metadata-to-ocm/" >}})
