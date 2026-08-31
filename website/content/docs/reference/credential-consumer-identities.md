---
title: "Credential Consumer Identities"
description: "Complete reference for OCM credential consumer identity types, their attributes, and credential properties."
icon: "🔑"
weight: 3
toc: true
---

This page is the technical reference for credential consumer identities — the key-value maps OCM uses to look up credentials for a given operation. For a high-level introduction, see [Credential System]({{< relref "docs/concepts/credential-system.md" >}}).

For the credential types that go in the `credentials:` field of each consumer entry,
see [Reference: Credential Types]({{< relref "credential-types.md" >}}).

## Overview

Every time OCM needs credentials (accessing a registry, signing a component version), it constructs a **lookup identity
** — a map of string attributes describing what it needs credentials for. The credential system then searches configured
consumers for a matching entry.

A consumer entry in `.ocmconfig` looks like this:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    consumers:
      - identity:
          type: <identity-type>
          # ... type-specific attributes
        credentials:
          - type: Credentials/v1
            properties:
            # ... key-value credential properties
```

The consumer identity type is extensible — any string in `Name` or `Name/Version` format can be used.
Plugins and integrations can introduce additional types (e.g. `AWSSecretsManager`, `HashiCorpVault`, `MavenRepository`).
The following types are defined by the core OCM modules:

| Identity Type                                 | Used For                                            |
|-----------------------------------------------|-----------------------------------------------------|
| [`OCIRegistry`](#ociregistry)                 | Authenticating against OCI registries               |
| [`HelmChartRepository`](#helmchartrepository) | Authenticating against Helm chart repositories      |
| [`Wget`](#wget)                               | Authenticating against plain HTTP/HTTPS servers     |
| [`S3Bucket`](#s3bucket)                       | Authenticating against S3 and S3-compatible buckets |
| [`GitHubRepository`](#githubrepository)       | Authenticating against the GitHub REST API          |
| [`RSA/v1alpha1`](#rsav1alpha1)                | Providing signing and verification keys             |

---

## OCIRegistry

Used when OCM accesses an OCI registry — pushing, pulling, or resolving component versions and resources.

### Identity Attributes

| Attribute  | Required | Description                                                                                                                         |
|------------|----------|-------------------------------------------------------------------------------------------------------------------------------------|
| `type`     | Yes      | Must be `OCIRegistry`                                                                                                               |
| `hostname` | Yes      | Registry hostname (e.g. `ghcr.io`, `registry.example.com`)                                                                          |
| `path`     | No       | Repository path. Supports glob patterns (`*` matches one path segment). If omitted, matches any path on the hostname.               |
| `scheme`   | No       | URL scheme (`https`, `http`, `oci`). If omitted, matches any scheme. If set, must match exactly.                                    |
| `port`     | No       | Port number as string. Default ports are applied when `scheme` is set: `https` and `oci` default to `443`, `http` defaults to `80`. |

### Credential Properties

| Property       | Description                                                            |
|----------------|------------------------------------------------------------------------|
| `username`     | Username for basic authentication                                      |
| `password`     | Password for basic authentication                                      |
| `accessToken`  | Bearer token sent directly to the registry (Docker token flow)         |
| `refreshToken` | OAuth2 refresh token exchanged for an access token before each request |

Token fields take precedence over `username`/`password` when both are present. Use [`OCICredentials/v1`]({{< relref "credential-types.md#ocicredentialsv1" >}}) for the full typed field reference.

### Matching Behavior

Matching runs three chained checks — all must pass:

1. **Path matcher** — compares `path` using `path.Match` (glob). `*` matches one segment, not across `/`. If the
   configured entry has no `path`, any request path is accepted.
2. **URL matcher** — compares `scheme`, `hostname`, and `port`. Applies default ports when a scheme is present (
   `https` → `443`, `http` → `80`).
3. **Equality matcher** — all remaining attributes (like `type`) must be exactly equal.

For detailed matching examples and edge cases, see [Tutorial: Understand Credential Resolution]({{< relref "docs/tutorials/credential-resolution.md" >}}).

### Examples

**Hostname only** — matches all paths on `ghcr.io`:

```yaml
- identity:
    type: OCIRegistry
    hostname: ghcr.io
  credentials:
    - type: OCICredentials/v1
      username: my-user
      password: ghp_token
```

**Hostname + path glob** — matches any single-segment path under `my-org/`:

```yaml
- identity:
    type: OCIRegistry
    hostname: ghcr.io
    path: my-org/*
  credentials:
    - type: OCICredentials/v1
      username: org-user
      password: ghp_org_token
```

**Hostname + scheme + port** — matches only HTTPS on a custom port:

```yaml
- identity:
    type: OCIRegistry
    hostname: registry.internal
    scheme: https
    port: "8443"
  credentials:
    - type: OCICredentials/v1
      username: internal-user
      password: internal_pass
```

---

## HelmChartRepository

Used when OCM accesses a remote Helm chart repository — pulling or resolving Helm charts referenced as resources. The
identity is derived from the Helm repository URL using the same URL-based attributes as `OCIRegistry`.

### Identity Attributes

| Attribute  | Required | Description                                                                    |
|------------|----------|--------------------------------------------------------------------------------|
| `type`     | Yes      | Must be `HelmChartRepository`                                                  |
| `hostname` | Yes      | Repository hostname (e.g. `charts.example.com`, `registry.example.com`)        |
| `path`     | No       | Repository path (e.g. `stable`). If omitted, matches any path on the hostname. |
| `scheme`   | No       | URL scheme (`https`, `http`, `oci`). If omitted, matches any scheme.           |
| `port`     | No       | Port number as string. If omitted, matches any port.                           |

### Credential Properties

| Property   | Description                  |
|------------|------------------------------|
| `username` | Repository username          |
| `password` | Repository password or token |

### Examples

**HTTPS Helm repository:**

```yaml
- identity:
    type: HelmChartRepository
    hostname: charts.example.com
    path: stable
  credentials:
    - type: HelmHTTPCredentials/v1
      username: helm-user
      password: helm-token
```

**OCI-based Helm repository:**

```yaml
- identity:
    type: HelmChartRepository
    hostname: registry.example.com
    scheme: oci
  credentials:
    - type: OCICredentials/v1
      username: registry-user
      password: registry-token
```

---

## Wget

Used when OCM fetches a resource over plain HTTP or HTTPS through the
[`Wget/v1` access type]({{< relref "input-and-access-types.md#wgetv1-access" >}}) and the
[`Wget/v1` input type]({{< relref "input-and-access-types.md#wgetv1-input" >}}). The identity is derived from the resource
`url`; the access type and the input type derive it identically, so a single consumer entry covers both.

### Identity Attributes

| Attribute  | Required | Description                                                                                                                            |
|------------|----------|----------------------------------------------------------------------------------------------------------------------------------------|
| `type`     | Yes      | Must be `Wget`                                                                                                                         |
| `hostname` | Yes      | Server hostname (e.g. `downloads.example.com`)                                                                                         |
| `path`     | No       | URL path without the leading `/`. Supports glob patterns (`*` matches one path segment). If omitted, matches any path on the hostname. |
| `scheme`   | No       | URL scheme (`https`, `http`). If omitted, matches any scheme. If set, must match exactly.                                              |
| `port`     | No       | Port number as string. During matching, default ports are applied when `scheme` is set: `https` defaults to `443`, `http` to `80`.     |

**Example derivation:** for `url: https://downloads.example.com/myapp/1.0.0/myapp.tar.gz`, the lookup identity is:

| Attribute  | Value                      |
|------------|----------------------------|
| `type`     | `Wget`                     |
| `hostname` | `downloads.example.com`    |
| `scheme`   | `https`                    |
| `path`     | `myapp/1.0.0/myapp.tar.gz` |

The URL carries no explicit port, so no `port` attribute is derived. Default ports stay implicit in the identity and
are applied by the URL matcher instead, so this identity matches a consumer entry with `port: "443"` as well as one
with no `port` at all. A URL that names its port (`https://downloads.example.com:8443/...`) does derive
`port: "8443"`.

### Credential Properties

| Property               | Description                                                                                                 |
|------------------------|-------------------------------------------------------------------------------------------------------------|
| `username`             | Username for HTTP basic authentication                                                                      |
| `password`             | Password for HTTP basic authentication                                                                      |
| `identityToken`        | Bearer token sent as `Authorization: Bearer <token>`. Takes precedence over Basic Auth.                     |
| `certificate`          | PEM-encoded client certificate for mutual TLS                                                               |
| `privateKey`           | PEM-encoded private key paired with `certificate`                                                           |
| `certificateAuthority` | PEM-encoded CA certificate used to verify the server certificate. Only applied together with `certificate`. |

Use [`WgetCredentials/v1`]({{< relref "credential-types.md#wgetcredentialsv1" >}}) for the typed field reference.

Basic Auth and a bearer token both set the `Authorization` header and are therefore mutually exclusive. When both are
configured, the bearer token wins and a warning is logged. The mutual TLS certificate is a transport-layer credential
and combines with either of them, but it only takes effect during a TLS handshake: supplying one for an `http://` URL
logs a warning and has no effect.

### Matching Behavior

The same three chained checks as [`OCIRegistry`](#ociregistry) apply: path glob, URL (scheme, hostname, port with
default-port handling), then exact equality on the remaining attributes.

{{< callout context="caution" >}}
The identity type is matched by exact string and is **unversioned**, so it must be written as `type: Wget`. Neither
`Wget/v1` (the name of the
[access and input type]({{< relref "input-and-access-types.md#wgetv1-access" >}})) nor the lowercase `wget` used by OCM v1
will match. A non-matching entry fails silently: no credentials are resolved and the request goes out unauthenticated,
so the symptom is a `401` from the server rather than a configuration error.
{{< /callout >}}

### Examples

**Hostname only.** Matches every download from that host:

```yaml
- identity:
    type: Wget
    hostname: downloads.example.com
  credentials:
    - type: WgetCredentials/v1
      username: download-user
      password: download-token
```

**Bearer token for a single path segment** (matches `artifacts/build.zip`, not `artifacts/ci/build.zip`):

```yaml
- identity:
    type: Wget
    hostname: api.example.com
    scheme: https
    path: artifacts/*
  credentials:
    - type: WgetCredentials/v1
      identityToken: eyJhbGciOi...
```

**Mutual TLS against an internal server:**

```yaml
- identity:
    type: Wget
    hostname: artifacts.internal
    scheme: https
    port: "8443"
  credentials:
    - type: WgetCredentials/v1
      certificate: |
        -----BEGIN CERTIFICATE-----
        MIIDdzCCAl+gAwIBAgIEbGVnYWw...
        -----END CERTIFICATE-----
      privateKey: |
        -----BEGIN PRIVATE KEY-----
        MIIEvQIBADANBgkqhkiG9w0BAQ...
        -----END PRIVATE KEY-----
      certificateAuthority: |
        -----BEGIN CERTIFICATE-----
        MIIDQTCCAimgAwIBAgITBmyf...
        -----END CERTIFICATE-----
```

For migrating a Wget consumer entry from OCM v1, covering the renamed identity type, the `pathprefix` to `path`
conversion, and the inverted authentication precedence, see
[Tutorial: Work with HTTP Resources]({{< relref "docs/tutorials/wget-http-resources.md#credential-changes" >}}).

---

## S3Bucket

Used when OCM reads an object from an S3 or S3-compatible bucket. This applies to the
[`S3Bucket/v1` access type]({{< relref "input-and-access-types.md#s3bucketv1-access" >}}) and to the
[`S3Bucket/v1` input type]({{< relref "input-and-access-types.md#s3bucketv1-input" >}}). OCM derives the identity from
`bucketName`, `objectKey` and the optional `endpoint`. The access type and the input type derive it the same way, so
one consumer entry covers both.

Credentials are optional. If no consumer entry matches, OCM gives no credentials to the AWS SDK. The SDK then uses its
default credential chain:

- the environment variables `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` and `AWS_SESSION_TOKEN`
- the shared AWS config files
- IAM instance roles and task roles

Use this path for in-cluster and CI setups. Short-lived role credentials are safer than static keys in `.ocmconfig`.

### Identity Attributes

| Attribute  | Required | Description                                                                                                                                           |
|------------|----------|-------------------------------------------------------------------------------------------------------------------------------------------------------|
| `type`     | Yes      | Must be `S3Bucket`                                                                                                                                    |
| `path`     | No       | Object location as `<bucketName>/<objectKey>`. Glob patterns are allowed. `*` matches one path segment. If you omit it, the entry matches any object. |
| `hostname` | No       | Host of the `endpoint`, for example `minio.internal`. OCM derives it only for an S3-compatible store. **Do not set it for AWS S3.**                   |
| `scheme`   | No       | Scheme of the `endpoint`: `https` or `http`. If you omit it, the entry matches any scheme. If you set it, it must match exactly.                      |
| `port`     | No       | Port of the `endpoint`, as a string. If `scheme` is set, matching applies the default port: `443` for `https`, `80` for `http`.                       |

**Example derivation (AWS S3).** An access or input specification sets `bucketName: acme-artifacts` and
`objectKey: datasets/reference/1.0.0/reference.parquet`, and sets no `endpoint`. OCM derives this lookup identity:

| Attribute | Value                                                       |
|-----------|-------------------------------------------------------------|
| `type`    | `S3Bucket`                                                  |
| `path`    | `acme-artifacts/datasets/reference/1.0.0/reference.parquet` |

**Example derivation (S3-compatible store).** Add `endpoint: https://minio.internal:9000`. The endpoint supplies the
URL attributes. The path still names the object:

| Attribute  | Value                                                       |
|------------|-------------------------------------------------------------|
| `type`     | `S3Bucket`                                                  |
| `scheme`   | `https`                                                     |
| `hostname` | `minio.internal`                                            |
| `port`     | `9000`                                                      |
| `path`     | `acme-artifacts/datasets/reference/1.0.0/reference.parquet` |

`region`, `mediaType`, `version` and `usePathStyle` take no part in credential resolution.

### Credential Properties

| Property          | Description                                              |
|-------------------|----------------------------------------------------------|
| `accessKeyId`     | AWS access key ID                                        |
| `secretAccessKey` | Secret access key paired with `accessKeyId`              |
| `sessionToken`    | Session token for temporary (STS) credentials. Optional. |

Use [`S3Credentials/v1`]({{< relref "credential-types.md#s3credentialsv1" >}}) for the typed field reference.

If an entry sets none of the three properties, OCM treats it as no credentials, and the AWS default credential chain
applies. If an entry sets any of them, OCM passes the entry to the AWS SDK unchanged. An incomplete pair therefore
fails in the SDK. It does not fall back to the default chain.

### Matching Behavior

The same three chained checks as [`OCIRegistry`](#ociregistry) apply:

1. Glob match on the path.
2. URL match on scheme, hostname and port, with default-port handling.
3. Exact match on the remaining attributes.

Two results of this are specific to S3:

- **Do not set `hostname` in an AWS entry.** The matcher compares hostnames for equality, and an AWS lookup identity
  has no hostname. An entry with `hostname: s3.amazonaws.com` therefore never matches. Scope AWS entries by `path`,
  or do not scope them at all.
- **`*` does not cross `/`.** Most object keys contain slashes. `path: acme-artifacts/*` matches
  `acme-artifacts/build.zip`, but it does not match `acme-artifacts/datasets/reference.parquet`. To cover a whole
  bucket, write the full depth (`acme-artifacts/*/*/*`), or omit `path` and scope the entry another way.

{{< callout context="caution" >}}
Write the identity type as `type: S3Bucket`. OCM matches the type as an exact string, and the type is **unversioned**.
`S3Bucket/v1` (the name of the
[access and input type]({{< relref "input-and-access-types.md#s3bucketv1-access" >}})) does not match. The `S3` type of
OCM v1 does not match either.

A wrong type gives no error message. OCM resolves no credentials, the AWS default credential chain takes over, and the
request uses what that chain finds, which is often nothing. AWS then reports an access-denied error or a
missing-credentials error, not a configuration error.
{{< /callout >}}

### Examples

**All objects in every bucket.** Use this form when one account owns everything that OCM reads:

```yaml
- identity:
    type: S3Bucket
  credentials:
    - type: S3Credentials/v1
      accessKeyId: <access-key-id>
      secretAccessKey: <secret-access-key>
```

**Temporary credentials for one object:**

```yaml
- identity:
    type: S3Bucket
    path: acme-artifacts/datasets/reference/1.0.0/reference.parquet
  credentials:
    - type: S3Credentials/v1
      accessKeyId: <temporary-access-key-id>
      secretAccessKey: <temporary-secret-access-key>
      sessionToken: <session-token>
```

**A self-hosted MinIO on a custom port.** The endpoint attributes separate it from AWS:

```yaml
- identity:
    type: S3Bucket
    scheme: https
    hostname: minio.internal
    port: "9000"
  credentials:
    - type: S3Credentials/v1
      accessKeyId: minio-user
      secretAccessKey: minio-password
```

### Migrating from OCM v1 {#s3bucket-migration-from-ocm-v1}

OCM v2 does **not** resolve OCM v1 consumer entries for S3. You must rewrite them. Four things changed:

| Aspect                | OCM v1                                          | OCM v2                                                 |
|-----------------------|-------------------------------------------------|--------------------------------------------------------|
| Identity type         | `S3`                                            | `S3Bucket`                                             |
| Object location       | `pathprefix`, set to `<bucket>/<key>/<version>` | `path`, set to `<bucketName>/<objectKey>` (no version) |
| Location matching     | Prefix match                                    | Glob match (`*` does not cross `/`)                    |
| Credential properties | `awsAccessKeyID`, `awsSecretAccessKey`, `token` | `accessKeyId`, `secretAccessKey`, `sessionToken`       |

```yaml
# OCM v1
- identity:
    type: S3
    pathprefix: acme-artifacts/datasets
  credentials:
    - type: Credentials
      properties:
        awsAccessKeyID: <access-key-id>
        awsSecretAccessKey: <secret-access-key>
```

```yaml
# OCM v2
- identity:
    type: S3Bucket
    path: acme-artifacts/datasets/*
  credentials:
    - type: S3Credentials/v1
      accessKeyId: <access-key-id>
      secretAccessKey: <secret-access-key>
```

The old **property** names are still accepted, but only in an untyped
[`Credentials/v1`]({{< relref "credential-types.md#directcredentialsv1" >}}) entry. There, OCM reads `awsAccessKeyID`,
`awsSecretAccessKey` and `token`, and maps them to `accessKeyId`, `secretAccessKey` and `sessionToken`. A typed
`S3Credentials/v1` entry accepts the new names only. The old **identity** has no alias. `type: S3` with a `pathprefix`
never matches, whichever credential type it carries.

For the matching access specification changes, see
[Input and Access Types: Migrating from OCM v1]({{< relref "input-and-access-types.md" >}}#s3-migration-from-ocm-v1).

---

## GitHubRepository

Used when OCM resolves or downloads a resource with a `GitHub/v1` access — resolving a ref to a commit or fetching a
commit's source archive via the GitHub REST API. The identity is derived from the access's `repoUrl`. Credentials are
optional; see the note on anonymous access under
[`GitHubCredentials/v1`]({{< relref "credential-types.md#githubcredentialsv1" >}}).

### Identity Attributes

| Attribute  | Required | Description                                                                                       |
|------------|----------|---------------------------------------------------------------------------------------------------|
| `type`     | Yes      | Must be `GitHubRepository`                                                                        |
| `hostname` | Yes      | Repository hostname (e.g. `github.com`, a GitHub Enterprise host)                                 |
| `path`     | No       | Repository path (e.g. `open-component-model/open-component-model`). If omitted, matches any path. |
| `scheme`   | No       | URL scheme (`https`, `http`). If omitted, matches any scheme.                                     |
| `port`     | No       | Port number as string. If omitted, the scheme's default applies (`https` → `443`, `http` → `80`). |

### Credential Properties

| Property | Description                                |
|----------|--------------------------------------------|
| `token`  | GitHub or GitHub Enterprise access token   |

### Examples

**github.com:**

```yaml
- identity:
    type: GitHubRepository
    hostname: github.com
    path: open-component-model/open-component-model
  credentials:
    - type: GitHubCredentials/v1
      token: ghp_example_token
```

**GitHub Enterprise host:**

```yaml
- identity:
    type: GitHubRepository
    hostname: git.example.corp
  credentials:
    - type: GitHubCredentials/v1
      token: ghe_example_token
```

Omitting `path` matches every repository on that host.

---

## RSA/v1alpha1

Used when OCM signs or verifies component versions with RSA keys.

### Identity Attributes

| Attribute   | Required | Description                                                                                                                                            |
|-------------|----------|--------------------------------------------------------------------------------------------------------------------------------------------------------|
| `type`      | Yes      | Must be `RSA/v1alpha1`                                                                                                                                 |
| `algorithm` | Yes      | Signing algorithm. Must be `RSASSA-PSS` (recommended) or `RSASSA-PKCS1-V1_5`.                                                                          |
| `signature` | Yes      | Logical signature name (e.g. `default`). Must match the `--signature` flag used with `ocm sign cv`. Defaults to `default` if not specified on the CLI. |

{{< callout context="caution" >}}
**All three attributes are required.** When OCM looks up signing credentials, it always constructs a lookup identity
with `type`, `algorithm`, and `signature`. If your consumer entry omits `algorithm`, the credential system will not find
a match — even though the signing algorithm defaults to `RSASSA-PSS` internally.

If you are unsure which algorithm to use, specify `algorithm: RSASSA-PSS`.
{{< /callout >}}

### Credential Properties

| Property            | Used For     | Description                          |
|---------------------|--------------|--------------------------------------|
| `privateKeyPEM`     | Signing      | Inline PEM-encoded private key       |
| `privateKeyPEMFile` | Signing      | Path to PEM-encoded private key file |
| `publicKeyPEM`      | Verification | Inline PEM-encoded public key        |
| `publicKeyPEMFile`  | Verification | Path to PEM-encoded public key file  |

You can specify both `privateKeyPEMFile` and `publicKeyPEMFile` in the same entry to use it for both signing and
verification.

When using the legacy `Credentials/v1` `properties:` map instead of `RSACredentials/v1`, the old snake_case keys
(`private_key_pem`, `private_key_pem_file`, `public_key_pem`, `public_key_pem_file`) are still accepted as a deprecated
backward-compatibility fallback.

### Matching Behavior

Unlike OCI identities, RSA signing identities use **strict equality matching** — every attribute in the lookup identity
must be present in the configured consumer identity with the exact same value. There is no glob or subset matching.

### Examples

**Signing and verification with default settings:**

```yaml
- identity:
    type: RSA/v1alpha1
    algorithm: RSASSA-PSS
    signature: default
  credentials:
    - type: RSACredentials/v1
      privateKeyPEMFile: /path/to/private-key.pem
      publicKeyPEMFile: /path/to/public-key.pem
```

**Multiple signature identities** (e.g. dev and prod):

```yaml
- identity:
    type: RSA/v1alpha1
    algorithm: RSASSA-PSS
    signature: dev
  credentials:
    - type: RSACredentials/v1
      privateKeyPEMFile: /path/to/dev/private-key.pem
      publicKeyPEMFile: /path/to/dev/public-key.pem
- identity:
    type: RSA/v1alpha1
    algorithm: RSASSA-PSS
    signature: prod
  credentials:
    - type: RSACredentials/v1
      privateKeyPEMFile: /path/to/prod/private-key.pem
      publicKeyPEMFile: /path/to/prod/public-key.pem
```

Sign with a specific identity:

```bash
ocm sign cv --signature dev <component-version>
ocm sign cv --signature prod <component-version>
```

**Using PKCS#1 v1.5 algorithm:**

```yaml
- identity:
    type: RSA/v1alpha1
    algorithm: RSASSA-PKCS1-V1_5
    signature: legacy
  credentials:
    - type: RSACredentials/v1
      privateKeyPEMFile: /path/to/private-key.pem
```

---

## Complete Configuration Example

A single `.ocmconfig` combining registry credentials (with Docker fallback) and signing credentials:

```yaml
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    consumers:
      # OCI registry — hostname catch-all
      - identity:
          type: OCIRegistry
          hostname: ghcr.io
        credentials:
          - type: OCICredentials/v1
            username: my-user
            password: ghp_token
      # RSA signing — default signature
      - identity:
          type: RSA/v1alpha1
          algorithm: RSASSA-PSS
          signature: default
        credentials:
          - type: RSACredentials/v1
            privateKeyPEMFile: /path/to/private-key.pem
            publicKeyPEMFile: /path/to/public-key.pem
    # Docker config fallback for registries not matched above
    repositories:
      - repository:
          type: DockerConfig/v1
          dockerConfigFile: "~/.docker/config.json"
```

## Discovering Credential Types at Runtime

Use `ocm describe types credentials` to list all credential types registered in your OCM installation — including any
added by installed plugins — and `ocm describe types credentials <type>` to inspect the fields of a specific type.

---

## Related Documentation

- [Concept: Credential System]({{< relref "docs/concepts/credential-system.md" >}}) — How the credential system works
- [Reference: Credential Types]({{< relref "credential-types.md" >}}) — All built-in typed credential types and their
  fields
- [Tutorial: Understand Credential Resolution]({{< relref "docs/tutorials/credential-resolution.md" >}}) — Step-by-step
  matching examples for OCI registries
- [How-To: Configure Credentials for Multiple Registries]({{< relref "docs/how-to/configure-multiple-credentials.md" >}}) — Task-oriented registry credential setup
- [How-To: Configure Credentials for Signing]({{< relref "configure-signing-credentials.md" >}}) — Task-oriented signing
  credential setup
