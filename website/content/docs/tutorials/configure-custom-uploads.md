---
title: "Configure Custom Uploads During Transfer"
description: "Learn how to route resources to a custom HTTP upload target during transfer using an uploader configuration, instead of embedding them as local blobs."
icon: "🚚"
weight: 63
toc: true
hasMermaid: true
---

By default, `ocm transfer` either leaves external resources where they are or, with
a local blob uploader configuration, downloads and re-embeds them into the target component version.
An **uploader configuration** gives you a third option: route a matching resource
through a custom transformer that streams it to an upload target of your choice and
rewrites the resource to point at the new location.

This tutorial configures the built-in **HTTP streaming uploader** to stream a
`Wget/v1` resource to an HTTP `PUT` endpoint during transfer.

## What You'll Learn

- How an uploader configuration matches a resource by access type and routes it to a custom target
- How the HTTP streaming uploader streams content to a `PUT` endpoint without buffering it
- How the transferred resource is rewritten to a `Wget/v1` access at the upload target
- How to template the target URL from the source resource

## How It Works

```mermaid
flowchart LR
    Source["Source repository<br/>(resource: Wget/v1 access)"]
    Transfer["ocm transfer cv<br/>+ uploader config"]
    Target["Target HTTP endpoint<br/>(PUT)"]
    Descriptor["Target component version<br/>(resource: Wget/v1 @ target URL)"]

    Source -- "stream bytes" --> Transfer -- "PUT" --> Target
    Transfer -- "rewrite access" --> Descriptor
```

When a resource's access type matches the `match` of the uploader, transfer streams the
resource's bytes straight from the source into an HTTP request to your target URL,
computes (or verifies) its digest as the bytes pass through, and records a new
`Wget/v1` access on the transferred resource pointing at the upload target.

**Estimated time:** ~10 minutes

## Prerequisites

- [OCM CLI]({{< relref "docs/getting-started/ocm-cli-installation.md" >}}) installed
- A component version containing a resource with `Wget/v1` access (see
  [Working with HTTP Resources]({{< relref "docs/tutorials/wget-http-resources.md" >}}))
- An HTTP endpoint that accepts `PUT` uploads and any credentials it requires
- A target OCM repository (an OCI registry or a CTF archive) for the component descriptor

## Scenario

- **Component:** `ocm.software/demo:1.0.0` with a resource `docs` using `Wget/v1` access
- **Source URL:** `https://source.example.com/artifacts/docs.tar`
- **Upload target:** `https://mytarget.example.com/uploads/artifacts/docs.tar`
- **Config file:** `.ocmconfig` in the working directory

## Tutorial Steps

{{< steps >}}
{{< step >}}

### Write the uploader configuration

Create `.ocmconfig` with an uploader config that matches `Wget/v1` resources
and streams them to your target, followed by a catch-all that copies every other
resource as a local blob:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: http.uploader.transfer.config.ocm.software/v1alpha1
    match: resource.access.isType("Wget/v1")
    targetURL: '${"https://mytarget.example.com/uploads" + url(resource.access.url).path}'
    method: PUT
  - type: localblob.uploader.transfer.config.ocm.software/v1alpha1
```

> **Note:** The CLI merges `.ocmconfig` from the current directory with your other OCM configuration (such as `$HOME/.ocmconfig`), so credentials and resolvers stay in effect.

The `targetURL` is a [CEL](https://cel.dev/) expression wrapped in `${…}`,
evaluated against the source resource, exposed as `resource`. Here
`url(resource.access.url).path` parses the source resource's URL with the inbuilt
`url()` function and takes its path (`/artifacts/docs.tar`), so the expression
resolves to `https://mytarget.example.com/uploads/artifacts/docs.tar`. You can
also use `resource.name`, `resource.version`, other `url()` parts such as
`url(resource.access.url).host`, `resource.extraIdentity.<key>`, and CEL
conditionals. The uploader is not limited to
wget sources: every field of the source access is exposed under
`resource.access.<field>` (e.g. `resource.access.imageReference` for an OCI
source), so you can route any access type to an HTTP target — see the
[Transfer Configuration reference]({{< relref "docs/reference/transfer-configuration/cel-expressions.md" >}}).

Uploaders are evaluated in declaration order and the first one that selects a
resource handles it, so the HTTP uploader takes the `Wget/v1` resource and the
`localblob.uploader` catch-all copies everything else as a local blob (the same
as having a local blob uploader entry in your OCM configuration). Without the catch-all, local blobs are still
copied and all other resources stay by reference.

{{< /step >}}

{{< step >}}

### Forward a checksum header (optional)

If the target should receive the resource's digest on the `PUT`, template a header
from `resource.digest`. Header values use the same `${…}` CEL expressions as
`targetURL`; a value without `${…}` is sent as a literal. `resource.digest` is only
present when the source resource carries a digest (for example when it is pinned
from the source via the checksum-http configuration), so keep the rule scoped so
every matched resource has one.

```yaml
  - type: http.uploader.transfer.config.ocm.software/v1alpha1
    match: resource.access.isType("Wget/v1")
    targetURL: '${"https://mytarget.example.com/uploads" + url(resource.access.url).path}'
    method: PUT
    header:
      # RFC 9530 Content-Digest: sha-256=:<base64>:
      Content-Digest: ['${contentDigestAlgorithm(resource.digest.hashAlgorithm) + "=:" + base64.encode(hex.decode(resource.digest.value)) + ":"}']
      # A simple non-standard checksum header carrying the raw hex value.
      X-Checksum-Sha256: ['${resource.digest.value}']
      # A static literal header is sent verbatim.
      X-Uploaded-By: ['ocm-transfer']
```

`resource.digest.value` is the hex digest and `resource.digest.hashAlgorithm` is the
OCM algorithm name (e.g. `SHA-256`). `contentDigestAlgorithm()` maps that name to the
RFC 9530 field key (`sha-256`), and `base64.encode(hex.decode(...))` converts the hex
digest to the base64 value RFC 9530 expects — see the
[Templating Headers reference]({{< relref "docs/reference/transfer-configuration/http-uploader.md" >}}#templating-headers).

{{< /step >}}

{{< step >}}

### Configure target credentials

If your upload endpoint requires authentication, add credentials for the target host.
The uploader resolves credentials for the target URL using the `Wget` consumer
identity, independently of the source resource:

```yaml
  - type: credentials.config.ocm.software
    consumers:
      - identity:
          type: Wget
          hostname: mytarget.example.com
        credentials:
          - type: Credentials/v1
            properties:
              username: uploader
              password: <token>
```

Add this entry to the `configurations` list in `.ocmconfig`. See
[`WgetCredentials/v1`]({{< relref "docs/reference/credential-types.md#wgetcredentialsv1" >}}) and
[Credential Consumer Identities]({{< relref "docs/reference/credential-consumer-identities.md" >}})
for details.

{{< /step >}}

{{< step >}}

### Run the transfer

Transfer the component version. The CLI picks up the uploader configuration from `.ocmconfig` in the current directory:

```bash
ocm transfer cv \
  ghcr.io/source-org/ocm//ocm.software/demo:1.0.0 \
  ghcr.io/target-org/ocm
```

During transfer you will see the streaming step in the progress output, labelled
with the resource and target host:

{{< details "Expected progress output" >}}
```text
✓ Transferring component versions...
    ✓ demo@1.0.0 [Stream docs to mytarget.example.com]
    ✓ demo@1.0.0 [Upload to OCI]
    ✓ Cleanup temp files
```
{{< /details >}}

The `[Stream docs to ...]` line is the HTTP streaming uploader performing the `PUT`.

{{< /step >}}

{{< step >}}

### Verify the rewritten access

Inspect the transferred component version. The `docs` resource now has a `Wget/v1`
access pointing at your upload target:

```bash
ocm get cv ghcr.io/target-org/ocm//ocm.software/demo:1.0.0 -o yaml
```

{{< details "Expected resource access" >}}
```yaml
resources:
  - name: docs
    type: blob
    access:
      type: Wget/v1
      url: https://mytarget.example.com/uploads/artifacts/docs.tar
    digest:
      hashAlgorithm: SHA-256
      normalisationAlgorithm: genericBlobDigest/v1
      value: <sha256-of-the-streamed-bytes>
```
{{< /details >}}

The digest was computed while the bytes were streamed to the target. If the source
resource already carried a digest, transfer instead verified it as the bytes passed
through and failed the transfer on any mismatch.

Note the published access carries only `url` (and `mediaType`) — not the `PUT`
method or any upload headers. Those apply to the upload request only, so a later
`ocm download` reads the object with a plain GET instead of re-issuing the write.

{{< /step >}}
{{< /steps >}}

## What you've learned

- An uploader configuration matches a resource by access type and streams it to a dedicated target during transfer.
- The `http.uploader.transfer.config.ocm.software/v1alpha1` uploader streams the resource straight to an HTTP `PUT` target without buffering it, and computes or verifies its digest inline.
- The transferred resource is rewritten to a `Wget/v1` **read** access at the upload target (`url` + `mediaType`); the upload request fields (`method`, `header`, `body`, `noRedirect`) drive the `PUT` only and are not recorded on the published access, so a later download reads the object with a plain GET.
- `targetURL` and `header` values are `${…}` CEL expressions over the source resource, so you can template the upload URL and forward headers such as a checksum from `resource.digest`.

## Troubleshooting

### Problem: `targetURL` fails to evaluate

**Cause:** The CEL expression references a field that is not available on the
resource, or is not valid CEL.

**Fix:** Reference only documented fields — `resource.name`, `resource.version`,
`resource.access.<field>` (e.g. `resource.access.url`, and `url(...)` parts such
as `url(resource.access.url).path`), `resource.extraIdentity.<key>`,
`resource.digest.value` — and quote string literals (`"https://..."`). A missing
field fails the transfer deliberately rather than producing a partial URL.

### Problem: The upload returns 401/403

**Cause:** No credentials matched the target host.

**Fix:** Add a `credentials.config.ocm.software` consumer with `type: Wget` and the
target `hostname`, as in Step 2.

### Problem: The transfer succeeds but nothing is uploaded

**Symptom:** The resource is stored as `LocalBlob/v1` in the target and the log
warns `uploader matched no resource`.

**Cause:** `match.accessType` names the access the resource would get in the
target (such as `LocalBlob/v1`) instead of its access in the source component
version.

**Fix:** Check the source with `ocm get cv <source> -o yaml` and set
`match.accessType` to the resource's `access.type` there, for example
`OCIImage/v1` for an `ociArtifact` access.

## Upload to JFrog Artifactory or Sonatype Nexus

To upload into Artifactory or Nexus repositories, use the vendor uploaders instead
of the HTTP uploader. They detect the repository type (Helm, Maven, npm, generic or
raw) through the server API and publish an access consumers can use with their own
tools. See
[Using Vendor-Specific APIs]({{< relref "docs/how-to/vendor-specific-apis/_index.md" >}}).

## Next steps

- [How-to: Transfer Helm Charts with OCM]({{< relref "docs/how-to/transfer-helm-charts.md" >}})
- [How-to: Transfer Components Across an Air Gap]({{< relref "docs/how-to/air-gap-transfer.md" >}})

## Related documentation

- [Reference: Transfer Configuration]({{< relref "docs/reference/transfer-configuration/_index.md" >}}) — full field reference for transfer and uploader configuration
- [Concept: Transfer and Transport]({{< relref "docs/concepts/transfer-concept.md" >}}) — how OCM moves component versions between repositories
- [Tutorial: Working with HTTP Resources]({{< relref "docs/tutorials/wget-http-resources.md" >}}) — the `Wget/v1` type produced by the uploader
