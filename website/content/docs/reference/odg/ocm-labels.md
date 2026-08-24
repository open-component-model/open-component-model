---
title: "OCM Label Index"
description: "ODG uses OCM labels to influence scanning behaviour, CVE rescoring, responsible assignment, and metadata."
weight: 190
toc: true
---
ODG uses [OCM labels](https://ocm.software/docs/reference/component-descriptor/#component-labels) to influence scanning behaviour, CVE rescoring, responsible assignment, and metadata.

---

## `odg.ocm.software/binary-scan-policy` v1 {#odgocmsoftwarebinary-scan-policy-v1}

Controls whether a binary vulnerability scan is skipped in ODG.

```yaml
labels:
  - name: odg.ocm.software/binary-scan-policy
    version: v1
    value:
      policy: "scan" | "skip"
      comment: "free-text string"
```

| Field | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `policy` | string | yes | `scan` runs the scan (default behaviour); `skip` bypasses the vulnerability scan for this resource. |
| `comment` | string | no | Human-readable explanation for skipping the scan. |

---

## `odg.ocm.software/source-scan-policy` v1 {#odgocmsoftwaresource-scan-policy-v1}

Controls whether SAST (Static Application Security Testing) source analysis is run in ODG. Usually `skip` is set when the pipeline already ran a SAST scan.

```yaml
labels:
  - name: odg.ocm.software/source-scan-policy
    version: v1
    value:
      policy: "scan" | "skip"
      comment: "free-text string"
```

The fields are identical to those of [odg.ocm.software/binary-scan-policy]({{< relref "/docs/reference/odg/ocm-labels.md" >}}#odgocmsoftwarebinary-scan-policy-v1).

---

## `security.ocm.software/risk-profile` v1 {#securityocmsoftwarerisk-profile-v1}

Describes the deployment context of a component or artefact. ODG uses this information to suggest adjusted CVE severity scores that reflect the actual exposure of the component.

All fields are optional. Fields that are omitted are treated as unknown and do not contribute to rescoring decisions.

```yaml
labels:
  - name: security.ocm.software/risk-profile
    version: v1
    value:
      network_exposure: "private" | "protected" | "public"    # maps to CVSS: Attack Vector (AV)
      authentication_enforced: true | false    # CVSS: Privileges Required (PR)
      user_interaction: "operator" | "end-user"    # CVSS: User Interaction (UI)
      confidentiality_requirement: "none" | "low" | "high"   # CVSS: Confidentiality Requirement (CR)
      integrity_requirement: "none" | "low" | "high"   # CVSS: Integrity Requirement (IR)
      availability_requirement: "none" | "low" | "high"   # CVSS: Availability Requirement (AR)
      comment: "free-text string"
```

| Field | Type | Description |
| :--- | :--- | :--- |
| `network_exposure` | string | How reachable the component is from a network perspective. `private`: not reachable from outside a private network. `protected`: reachable from a restricted network or behind authentication. `public`: reachable from the internet. |
| `authentication_enforced` | boolean | Whether all access to the component requires authentication. |
| `user_interaction` | string | Who interacts with the component. `operator`: only operators/administrators. `end-user`: arbitrary end users. |
| `confidentiality_requirement` | string | How sensitive the data processed by the component is (`none`, `low` or `high`). |
| `integrity_requirement` | string | How critical correct operation of the component is (`none`, `low` or `high`). |
| `availability_requirement` | string | How critical continuous availability of the component is (`none`, `low` or `high`). |
| `comment` | string | Human-readable explanation of the categorisation choices. |

---

## `odg.ocm.software/responsibles` v1 {#odgocmsoftwareresponsibles-v1}

Explicitly declares who is responsible for a component or artefact.

```yaml
labels:
  - name: odg.ocm.software/responsibles
    version: v1
    value:
      - type: "githubUser"
        username: "some-github-handle"
        github_hostname: "github.com"    # optional, defaults to the hostname defined in the source access
      - type: "githubTeam"
        teamname: "my-org/my-team"
        github_hostname: "github.com"    # optional, defaults to the hostname defined in the source access
      - type: "codeowners"
```

| Type | Required fields | Description |
| :--- | :--- | :--- |
| `githubUser` | `username` | A specific GitHub user. |
| `githubTeam` | `teamname` | A GitHub team in `org/team` format. |
| `codeowners` | *(none)* | Resolves responsibles from the CODEOWNERS file in the component's source repository. |

---

## `odg.ocm.software/purposes` v1

Tags a resource with a set of named functional purposes. ODG uses this to discover resources that serve a specific role within a component.

```yaml
labels:
  - name: odg.ocm.software/purposes
    version: v1
    value:
      - lint
      - sast
      - pybandit
```

Currently the following values are recognised:

| Value | Effect |
|:---|:---|
| `sast` | The linting report |
