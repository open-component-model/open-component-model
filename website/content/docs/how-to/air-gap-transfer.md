---
title: "Transfer Components across an Air Gap"
description: "Transfer a signed OCM component version into an air-gapped registry via a CTF archive."
weight: 1
toc: true
---

## Goal

Transfer a signed component version from a source registry into an air-gapped target registry using a CTF archive as the transport medium. An air-gapped environment is a network that is physically isolated from untrusted networks such as the public internet.

## You'll end up with

- A verified, signed component version available in your air-gapped registry
- All resource artifacts (container images, Helm charts) copied into the target registry

**Estimated time:** ~10 minutes

## Prerequisites

- [OCM CLI]({{< relref "docs/getting-started/ocm-cli-installation.md" >}}) installed
- A signed component version in a source registry or CTF archive
- The public key used to sign the component version, configured in `.ocmconfig` (see [Signing and Verification]({{< relref "docs/tutorials/signing/plain.md" >}}) for setup)
- Write access to the target registry in the air-gapped environment

## Steps

{{< steps >}}
{{< step >}}

### Verify the source component

Confirm integrity and provenance before transferring. The OCM CLI resolves verification credentials from your [`.ocmconfig`]({{< relref "docs/getting-started/ocm-cli-installation.md" >}}) automatically. For background on how signing works, see [Signing and Verification]({{< relref "docs/tutorials/signing/plain.md" >}}).

```bash
ocm verify cv <source-repository>//<component-name>:<version>
```

To verify a specific signature by name:

```bash
ocm verify cv --signature <signature-name> <source-repository>//<component-name>:<version>
```

You should see: `SIGNATURE VERIFICATION SUCCESSFUL` and exit code `0`. For detailed verification options, see [Signing and Verification]({{< relref "docs/tutorials/signing/plain.md" >}}).

> **Tip: CTF as source**
>
> If your source is a [CTF archive]({{< relref "docs/concepts/transfer-concept.md" >}}) rather than a registry, use the archive path:
>
> ```bash
> ocm verify cv ctf::<path/to/source.ctf>//<component-name>:<version>
> ```

{{< /step >}}
{{< step >}}

### Transfer to a CTF archive

Create a self-contained [CTF archive]({{< relref "docs/concepts/transfer-concept.md" >}}) that bundles all resource artifacts and transitively referenced [component versions]({{< relref "docs/concepts/component-identity.md" >}}). See [Transfer and Transport]({{< relref "docs/concepts/transfer-concept.md" >}}) for details on the `--copy-resources` flag.

```bash
ocm transfer cv \
  --copy-resources \
  --recursive \
  <source-repository>//<component-name>:<version> \
  ctf::<path/to/airgap-transport.ctf>
```

You should see:

- A progress bar while artifacts are downloaded
- Exit code `0` and the CTF archive created at the specified path

> **Tip: Working with the archive directly**
>
> The CTF archive is a fully functional OCM repository. You can inspect component versions or [download resources]({{< relref "docs/how-to/download-resources-from-component-versions.md" >}}) directly from it without importing into a registry first:
>
> ```bash
> ocm get cv ctf::<path/to/airgap-transport.ctf>
> ocm download resource ctf::<path/to/airgap-transport.ctf>//<component-name>:<version> \
>   --identity name=<resource-name> --output <output-path>
> ```

{{< /step >}}
{{< step >}}

### Move the archive across the air gap

Move the CTF archive to the air-gapped environment using whatever mechanism is available. This step does not involve the OCM CLI.

```bash
# Examples - use whatever method your environment allows:
scp -r airgap-transport.ctf user@jumphost:/transfer/
# or copy to USB media
cp -r airgap-transport.ctf /media/usb-drive/
# or create a compressed archive first
tar czf airgap-transport.ctf.tar.gz airgap-transport.ctf
```

{{< /step >}}
{{< step >}}

### Import into the target registry

On the air-gapped side, transfer the CTF archive into the target registry. The target registry must have [credentials configured]({{< relref "docs/how-to/configure-multiple-credentials.md" >}}) in your `.ocmconfig`.

```bash
ocm transfer cv \
  --copy-resources \
  --recursive \
  ctf::<path/to/airgap-transport.ctf>//<component-name>:<version> \
  <target-registry>
```

You should see:

- A progress bar while artifacts are uploaded
- Exit code `0` and the component available in the target registry

{{< /step >}}
{{< step >}}

### Verify in the target registry

Confirm the [component version]({{< relref "docs/concepts/component-identity.md" >}}) is available in the target registry:

```bash
ocm get cv <target-registry>//<component-name>:<version>
```

<details>
  <summary>Expected output</summary>

```text
 COMPONENT        VERSION   PROVIDER
 <component-name> <version> <provider>
```
</details>

Then verify the signature to confirm it survived the [transfer]({{< relref "docs/concepts/transfer-concept.md" >}}) intact:

```bash
ocm verify cv <target-registry>//<component-name>:<version>
```

You should see: `SIGNATURE VERIFICATION SUCCESSFUL`.

{{< /step >}}
{{< /steps >}}

## Troubleshooting

If you encounter authentication or credential errors during transfer or verification,
see [Credentials in .ocmconfig]({{< relref "docs/how-to/configure-multiple-credentials.md" >}}).

If signature verification fails after transfer, ensure the public key in your `.ocmconfig` matches the key used to sign the component.
See [Signing and Verification]({{< relref "docs/tutorials/signing/plain.md" >}}).

## Cleanup

Remove the temporary CTF archive after successful transfer and verification:

```bash
rm -rf airgap-transport.ctf
```

{{< callout context="caution" title="Caution" >}}
Only delete the archive after you have verified the component in the target registry. The archive is your only copy of the artifacts until the import is confirmed.
{{< /callout >}}

## Next Steps

- [How-To: Deploy a Helm Chart (Bootstrap)]({{< relref "docs/tutorials/deploy-helm-chart-bootstrap.md" >}}) - Deploy component resources into a Kubernetes cluster

## Related Documentation

- [Concept: Transfer and Transport]({{< relref "docs/concepts/transfer-concept.md" >}}) - Understand the transfer model, resource handling, and signature preservation
- [Tutorial: Signing and Verification]({{< relref "docs/tutorials/signing/plain.md" >}}) - Learn how to sign and verify component versions
