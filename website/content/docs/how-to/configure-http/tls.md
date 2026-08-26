---
title: "TLS and Custom CA"
description: "Trust a private certificate authority for OCM registry traffic, or disable TLS verification for local and development registries."
icon: "🔒"
weight: 4
toc: true
---

## Goal

Trust an internal or self-signed CA for OCM registry traffic without
disabling TLS verification — or, as a last resort for local dev registries,
opt out of verification for a specific host.

## Prerequisites

- [OCM CLI]({{< relref "/docs/getting-started/ocm-cli-installation.md" >}}) installed
- PEM-encoded CA certificate for the registry (for the `SSL_CERT_FILE` path)

## Steps

{{< steps >}}

{{< step >}}
**Trust a private CA without disabling TLS verification**

The OCM CLI uses Go's standard `crypto/tls` stack; the system root CA pool is
consulted automatically. To trust an additional internal CA, point
`SSL_CERT_FILE` and/or `SSL_CERT_DIR` at your bundle. TLS verification stays
on — the CLI just learns about an extra root.

```bash
# Single PEM bundle — covers most internal-CA setups.
export SSL_CERT_FILE=/etc/ocm/corp-ca.pem
ocm get cv registry.corp/my-org//ocm.software/demos/podinfo:6.8.0
```

```bash
# Multiple bundles — point at a directory of *.pem / *.crt files.
export SSL_CERT_DIR=/etc/ocm/ca.d
ocm get cv registry.corp/my-org//ocm.software/demos/podinfo:6.8.0
```

{{< callout context="caution" title="Replacement, not merge" >}}
Each variable **replaces** the corresponding
built-in default list inside Go's loader (`crypto/x509`'s `loadOnDiskRoots`).
`SSL_CERT_FILE` replaces the built-in list of fallback bundle paths
(`/etc/ssl/certs/ca-certificates.crt`, `/etc/pki/tls/certs/ca-bundle.crt`, …).
`SSL_CERT_DIR` replaces the built-in list of fallback directories
(`/etc/ssl/certs`, `/etc/pki/tls/certs`).

Because the file and directory channels are independent, **setting only one**
of the two leaves the other channel pulling in system roots — on a typical
Debian/Ubuntu host `SSL_CERT_FILE=/etc/ocm/corp-ca.pem` alone still trusts
public registries. **Setting both** disables all system-root fallbacks; only
your custom CAs are trusted. Public registries then fail with
`x509: certificate signed by unknown authority`.
{{< /callout >}}

To keep both your private CA **and** the system roots, concatenate them:

```bash
cat /etc/ssl/certs/ca-certificates.crt /etc/ocm/corp-ca.pem \
  > /etc/ocm/combined-ca.pem
export SSL_CERT_FILE=/etc/ocm/combined-ca.pem
```

Or drop both into a directory:

```bash
mkdir -p /etc/ocm/ca.d
cp /etc/ssl/certs/ca-certificates.crt /etc/ocm/ca.d/system.pem
cp /etc/ocm/corp-ca.pem               /etc/ocm/ca.d/corp.pem
export SSL_CERT_DIR=/etc/ocm/ca.d
```

{{< callout context="caution" >}}
**Platform differences.** On macOS and Windows, Go normally delegates
verification to the platform trust store (Keychain / CryptoAPI). Setting
**either** `SSL_CERT_FILE` or `SSL_CERT_DIR` switches Go to its pure-Go
on-disk loader and bypasses the platform store entirely. To re-enable the
platform store and ignore the env vars, set
`GODEBUG=x509sslcertoverrideplatform=0` and install the corp CA into the
platform store instead.
{{< /callout >}}
{{< /step >}}

{{< step >}}
**Disable TLS verification for a local or development registry**

When a local registry uses a self-signed certificate that you cannot or do not
want to trust via a CA bundle, use `insecureSkipVerify`. Always scope it to
the specific host:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    hosts:
      "registry.kind.local:5001":
        insecureSkipVerify: true
```

{{< callout context="danger" >}}
`insecureSkipVerify: true` disables certificate chain and hostname checks —
connections become vulnerable to active man-in-the-middle attacks. **Never
enable this against a public registry or in production.** OCM logs a warning
at startup and on the first request to each new host whenever this is active.
{{< /callout >}}

{{< callout context="tip" >}}
Prefer `SSL_CERT_FILE` / `SSL_CERT_DIR` over `insecureSkipVerify` whenever
the registry has a real certificate — even a self-signed one — because TLS
verification stays active and the CLI will still detect tampered or expired
certs. Reach for `insecureSkipVerify` only when you have no certificate to
pin against (e.g. a bare `kind` cluster with no cert at all).
{{< /callout >}}
{{< /step >}}

{{< /steps >}}

## Troubleshooting

### `x509: certificate signed by unknown authority`

The registry's certificate is signed by a CA not in the system trust store,
and no env var override has been set. Fix in order of preference:

1. Point `SSL_CERT_FILE` at a PEM bundle containing the issuing CA — TLS
   verification stays enabled.
2. Install the CA into the system trust store so all tools on the machine
   accept it.
3. Set `insecureSkipVerify: true` for that host only as a last resort.

### `SSL_CERT_FILE` is set but the registry still fails verification

Common causes:

- The variable was set in a different shell or after the OCM process started.
- The PEM bundle contains the **server** certificate rather than its issuing CA.
- `SSL_CERT_DIR` points at files without `.pem`, `.crt`, or `.cer` extensions.
- The hostname in the URL does not match a Subject Alternative Name on the cert — trusting the CA does not bypass hostname verification.

Verify the chain:

```bash
openssl s_client -connect host:port -showcerts
```

Run OCM with `--loglevel debug` — a TLS error reports the leaf cert subject
(hostname mismatch) versus the issuer (chain validation failure).

### Setting `SSL_CERT_FILE` breaks public registries

Both env vars were set and the custom bundle contains only the corp CA, so
public CAs disappeared from the trust pool. Concatenate the system bundle:

```bash
cat /etc/ssl/certs/ca-certificates.crt /etc/ocm/corp-ca.pem \
  > /etc/ocm/combined-ca.pem
export SSL_CERT_FILE=/etc/ocm/combined-ca.pem
```

On macOS / Windows: install the corp CA into the platform store and set
`GODEBUG=x509sslcertoverrideplatform=0` to re-enable the platform verifier.

### `WARN msg="HTTP transport built with InsecureSkipVerify=true"` at startup

This is informational — OCM logs the warning whenever TLS verification is
disabled. If unexpected, check whether `insecureSkipVerify: true` is set at
the global level rather than under a specific host entry.

## Reference

[HTTP Client Configuration Reference — TLS Trust]({{< relref "docs/reference/http-client-configuration.md#tls-trust-ssl_cert_file-and-ssl_cert_dir" >}})

## Related

- [Per-Host Overrides]({{< relref "per-host.md" >}}) — scope `insecureSkipVerify` to specific registries
- [HTTP Timeouts]({{< relref "timeouts.md" >}}) — set timeouts alongside TLS settings
