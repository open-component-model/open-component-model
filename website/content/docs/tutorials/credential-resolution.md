---
title: "Understand Credential Resolution"
description: "Learn how OCM resolves credentials by building a config and observing which credentials get picked for different requests."
icon: "🔑"
weight: 60
toc: true
---

## Overview

Every time OCM accesses a registry, it constructs a lookup identity from the request and finds the first matching consumer entry in your credential config. This tutorial walks you through that with real commands against a local registry — you'll see which configs match and which don't, and why.

For how the matching algorithm works end-to-end, see [Credential System]({{< relref "/docs/concepts/credential-system.md" >}}).

**Estimated time:** ~15 minutes

## What You'll Learn

- Write a consumer entry that matches a local registry
- See the effect of hostname, port, and scheme mismatches firsthand
- Understand why `scheme: http` without an explicit port fails on non-standard ports
- Understand why `scheme: oci` and `scheme: https` are equivalent
- Know when `path` on a consumer entry does and does not apply

## Prerequisites

- [OCM CLI]({{< relref "/docs/getting-started/ocm-cli-installation.md" >}}) installed
- Docker installed and running

## Setup

Start a local registry with basic auth and push a test component. All examples use this registry.

{{< steps >}}
{{< step >}}

### Start the local registry

```bash
export REGISTRY_PORT=15020
export REGISTRY_USER=testuser
export REGISTRY_PASS=testpassword
export WORKDIR=$(mktemp -d)
export REGISTRY_REF="http://localhost:${REGISTRY_PORT}//ocm.software/cred-tutorial:1.0.0"

mkdir -p "$WORKDIR/auth"
docker run --rm --entrypoint htpasswd httpd:2 \
  -Bbn "$REGISTRY_USER" "$REGISTRY_PASS" > "$WORKDIR/auth/htpasswd"

docker run -d \
  -p "${REGISTRY_PORT}:5000" \
  -v "$WORKDIR/auth:/auth" \
  -e REGISTRY_AUTH=htpasswd \
  -e REGISTRY_AUTH_HTPASSWD_REALM=Test \
  -e REGISTRY_AUTH_HTPASSWD_PATH=/auth/htpasswd \
  registry:2
```

{{< /step >}}
{{< step >}}

### Push a test component

```bash
cat > "$WORKDIR/constructor.yaml" <<'EOF'
components:
- name: ocm.software/cred-tutorial
  version: 1.0.0
  provider:
    name: ocm.software
  resources:
  - name: payload
    version: 1.0.0
    type: blob
    input:
      type: utf8/v1
      text: "credential resolution tutorial test"
EOF

ocm add cv --repository "ctf::$WORKDIR/test.ctf" \
  --constructor "$WORKDIR/constructor.yaml"

cat > "$WORKDIR/push.yaml" <<EOF
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    consumers:
      - identities:
          - type: OCIRegistry
            hostname: localhost
            port: "$REGISTRY_PORT"
            scheme: http
        credentials:
          - type: Credentials/v1
            properties:
              username: $REGISTRY_USER
              password: $REGISTRY_PASS
EOF

ocm --config "$WORKDIR/push.yaml" transfer component-version \
  "ctf::$WORKDIR/test.ctf//ocm.software/cred-tutorial:1.0.0" \
  "http://localhost:${REGISTRY_PORT}" --copy-resources
```

{{< /step >}}
{{< /steps >}}

Throughout this tutorial, verify each config with:

```bash
ocm --config <config-file> get component-version "$REGISTRY_REF"
```

A correct credential match prints the component version. A mismatch returns `401 Unauthorized`.

## Example A: Hostname-Only Match

A consumer entry with only a `hostname` (no `path`) is a catch-all for that host — it matches every request to that hostname regardless of anything else.

```bash
cat > "$WORKDIR/example-a.yaml" <<EOF
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    consumers:
      - identities:
          - type: OCIRegistry
            hostname: localhost
            port: "$REGISTRY_PORT"
            scheme: http
        credentials:
          - type: Credentials/v1
            properties:
              username: $REGISTRY_USER
              password: $REGISTRY_PASS
EOF

ocm --config "$WORKDIR/example-a.yaml" get component-version "$REGISTRY_REF"
# → success: component version printed
```

Change `hostname: localhost` to `hostname: docker.io` and retry — the request still targets `localhost`, so `docker.io` does not match:

```bash
# docker.io ≠ localhost → no credentials found → 401
ocm --config "$WORKDIR/example-a-wrong-host.yaml" get component-version "$REGISTRY_REF"
```

**Takeaway:** Hostname must match exactly. A single-attribute mismatch means no consumer is found.

## Example B: Path Matching — Conceptual Only

Consumer entries support a `path` attribute with glob matching. `*` matches exactly one path segment and never crosses a `/`:

| Configured path | Request path        | Matches? | Why                          |
|-----------------|---------------------|----------|------------------------------|
| `my-org/repo`   | `my-org/repo`       | Yes      | Exact                        |
| `my-org/*`      | `my-org/staging`    | Yes      | `*` matches one segment      |
| `my-org/*`      | `my-org/team/repo`  | No       | `*` won't span `/`           |
| `my-org`        | `my-org/production` | No       | No prefix matching           |
| `*/*`           | `my-org/repo`       | Yes      | Two-level wildcard           |

{{< callout context="caution" title="Path matching does not apply to component-version operations" icon="outline/alert-triangle" >}}
For `ocm get component-version` and `ocm transfer component-version`, OCM builds the lookup identity from the registry base URL (`scheme + hostname + port`) **only**. The component namespace is never included. A consumer entry with a non-empty `path` will therefore **never match** a component-version operation. Use hostname-only entries for component registry access.
{{< /callout >}}

When multiple wildcard entries could match the same request, the first one found wins — there is no most-specific-pattern-wins ranking. Keep overlapping wildcard patterns off the same host.

## Example C: Scheme and Port Normalization

When a `scheme` is present, OCM applies default ports before comparing:

- `scheme: https` → default port `443`
- `scheme: http` → default port `80`

This matters when the registry runs on a non-standard port.

**Explicit port, no scheme — matches:**

```bash
cat > "$WORKDIR/example-c-port.yaml" <<EOF
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    consumers:
      - identities:
          - type: OCIRegistry
            hostname: localhost
            port: "$REGISTRY_PORT"
        credentials:
          - type: Credentials/v1
            properties:
              username: $REGISTRY_USER
              password: $REGISTRY_PASS
EOF

ocm --config "$WORKDIR/example-c-port.yaml" get component-version "$REGISTRY_REF"
# → success: port 15020 matches explicitly
```

**Scheme `http` without port — does not match:**

```bash
cat > "$WORKDIR/example-c-http.yaml" <<EOF
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    consumers:
      - identities:
          - type: OCIRegistry
            hostname: localhost
            scheme: http
        credentials:
          - type: Credentials/v1
            properties:
              username: $REGISTRY_USER
              password: $REGISTRY_PASS
EOF

ocm --config "$WORKDIR/example-c-http.yaml" get component-version "$REGISTRY_REF"
# → 401 Unauthorized — http defaults port to 80; 80 ≠ 15020
```

**Scheme `http` with explicit port — matches:**

```bash
cat > "$WORKDIR/example-c-scheme-port.yaml" <<EOF
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    consumers:
      - identities:
          - type: OCIRegistry
            hostname: localhost
            scheme: http
            port: "$REGISTRY_PORT"
        credentials:
          - type: Credentials/v1
            properties:
              username: $REGISTRY_USER
              password: $REGISTRY_PASS
EOF

ocm --config "$WORKDIR/example-c-scheme-port.yaml" get component-version "$REGISTRY_REF"
# → success
```

**Takeaway:** For non-standard ports, always pair `scheme` with an explicit `port`. Without `port`, the scheme's default (80 or 443) is used, not the port in the request URL.

## Example D: The `oci` Scheme

`oci` normalizes to `https` before comparison — the two are interchangeable, and the default port `443` applies to both.

```bash
cat > "$WORKDIR/example-d-oci.yaml" <<EOF
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    consumers:
      - identities:
          - type: OCIRegistry
            hostname: localhost
            port: "$REGISTRY_PORT"
            scheme: oci
        credentials:
          - type: Credentials/v1
            properties:
              username: $REGISTRY_USER
              password: $REGISTRY_PASS
EOF

ocm --config "$WORKDIR/example-d-oci.yaml" get component-version "$REGISTRY_REF"
# → 401 Unauthorized — oci normalizes to https; https ≠ http (registry runs plain HTTP)
```

**Takeaway:** `scheme: oci` and `scheme: https` are identical after normalization. Use `scheme: oci` only for HTTPS registries. Only `http` is genuinely different.

## Example E: When Nothing Matches

The three most common mismatches, each producing `401 Unauthorized`:

**Wrong hostname:**

```bash
cat > "$WORKDIR/example-e-host.yaml" <<EOF
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    consumers:
      - identities:
          - type: OCIRegistry
            hostname: quay.io
            port: "$REGISTRY_PORT"
            scheme: http
        credentials:
          - type: Credentials/v1
            properties:
              username: $REGISTRY_USER
              password: $REGISTRY_PASS
EOF
ocm --config "$WORKDIR/example-e-host.yaml" get component-version "$REGISTRY_REF"
# → 401 Unauthorized — quay.io ≠ localhost
```

**Scheme mismatch (configured `https`, registry is `http`):**

```bash
cat > "$WORKDIR/example-e-scheme.yaml" <<EOF
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    consumers:
      - identities:
          - type: OCIRegistry
            hostname: localhost
            port: "$REGISTRY_PORT"
            scheme: https
        credentials:
          - type: Credentials/v1
            properties:
              username: $REGISTRY_USER
              password: $REGISTRY_PASS
EOF
ocm --config "$WORKDIR/example-e-scheme.yaml" get component-version "$REGISTRY_REF"
# → 401 Unauthorized — https ≠ http
```

**Wrong port:**

```bash
cat > "$WORKDIR/example-e-port.yaml" <<EOF
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    consumers:
      - identities:
          - type: OCIRegistry
            hostname: localhost
            port: "9999"
            scheme: http
        credentials:
          - type: Credentials/v1
            properties:
              username: $REGISTRY_USER
              password: $REGISTRY_PASS
EOF
ocm --config "$WORKDIR/example-e-port.yaml" get component-version "$REGISTRY_REF"
# → 401 Unauthorized — 9999 ≠ 15020
```

## Example F: Indirect Credentials (Plugin-Backed)

The difference between direct and indirect consumers is the credential `type`. When OCM encounters a type it doesn't recognize as built-in, it calls a plugin to resolve it — the config structure is otherwise identical.

The following is hypothetical — `HashiCorpVault/v1alpha1` would be provided by a third-party plugin:

```yaml
consumers:
  - identities:
      - type: OCIRegistry
        hostname: quay.io
    credentials:
      - type: HashiCorpVault/v1alpha1
        serverURL: "https://myvault.example.com/"
        path: "my/path/to/my/secret"
  - identities:
      - type: HashiCorpVault/v1alpha1
        hostname: myvault.example.com
    credentials:
      - type: DirectCredentials/v1
        properties:
          role_id: "repository.vault.com-role"
          secret_id: "repository.vault.com-secret"
```

Resolution for `quay.io`: OCM matches the Vault consumer → the Vault plugin returns a `HashiCorpVault/v1alpha1` identity for `myvault.example.com` → OCM resolves `role_id`/`secret_id` from the direct entry → Vault returns the final OCI credentials. Chains of arbitrary depth are supported; cycles are rejected at config load time.

## Troubleshooting

### `401 Unauthorized` despite a consumer entry that looks correct

Check each attribute in order:

1. **`hostname`** — must match exactly
2. **`scheme`** — if set, must match after normalization (`oci` = `https`; only `http` differs)
3. **`port`** — if `scheme` is set without `port`, the scheme's default applies (`https`→443, `http`→80)
4. **`path`** — if set, never matches a component-version operation (lookup uses base URL only)

### `401 Unauthorized` when Docker fallback should work

```bash
docker login <registry-hostname>
```

Then retry the OCM command.

## What You've Learned

- A hostname-only consumer entry matches every request to that host
- Setting `scheme` without `port` uses the scheme's default — always pair them for non-standard ports
- `oci` and `https` are equivalent after normalization; only `http` is genuinely different
- `path` on a consumer never matches `ocm get/transfer component-version` — only base URL is used in the lookup identity
- When multiple wildcard entries could match, the first found wins — keep patterns non-overlapping

## Next Steps

- [How-To: Configure Credentials for Multiple Repositories]({{< relref "configure-multiple-credentials.md" >}}) — Configure OCM for multiple OCI registries
- [How-To: Migrate v1 Credentials to v2]({{< relref "legacy-credential-compatibility.md" >}}) — Migrate an existing OCM v1 `.ocmconfig` file

## Related Documentation

- [Concept: Credential System]({{< relref "/docs/concepts/credential-system.md" >}}) — How the credential system works end-to-end
- [Reference: Consumer Identities]({{< relref "/docs/reference/credential-consumer-identities.md" >}}) — Complete reference for all identity types
