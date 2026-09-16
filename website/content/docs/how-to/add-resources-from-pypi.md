---
title: "Add Resources from PyPI"
description: "Reference a Python distribution from a PyPI-compatible index in a component version with the PyPI access type."
icon: "🐍"
weight: 19
toc: true
---

## Goal

Add a Python distribution (a wheel or source distribution) from a
PyPI-compatible index to a component version, using the
[`PyPI/v1alpha1` access type]({{< relref "docs/reference/input-and-access-types.md#pypiv1alpha1-access" >}}).
The access stores the index URL, the project name and the version; the files
stay on the index until something asks for them.

## You'll end up with

- A component version in a local transport archive, with the PyPI distribution declared as a resource
- A resource carrying a digest over the downloaded archive, ready to be signed
- That archive downloaded back out, to confirm the round trip works

**Estimated time:** ~10 minutes

## Prerequisites

- [OCM CLI]({{< relref "docs/getting-started/ocm-cli-installation.md" >}}) installed

This guide reads the public PyPI index, so it runs end-to-end without
credentials — start at step 2. Step 1 covers reaching a private index.

## Steps

{{< steps >}}

{{< step >}}

### Authenticate against a private index {#authenticate-against-a-private-index}

*Optional — skip this if the index is public.*

A public index is readable anonymously. For a private index (a Nexus or
Artifactory PyPI-hosted repository, or an authenticated mirror),
[configure credentials]({{< relref "docs/how-to/configure-multiple-credentials.md" >}}) of type
`PyPICredentials/v1`. OCM matches them by a consumer identity of type
`PyPIRepository` derived from `indexUrl`:

```bash
cat > .ocmconfig << 'EOF'
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    consumers:
      - identity:
          type: PyPIRepository
          hostname: pypi.org
          scheme: https
        credentials:
          - type: PyPICredentials/v1
            username: __token__
            password: pypi-your-api-token
EOF
```

An API token is configured as the `__token__` username with the token as the
password. Username/password and a bearer `identityToken` are also supported.
See [Credential Consumer Identities: PyPIRepository]({{< relref "docs/reference/credential-consumer-identities.md#pypirepository" >}})
for the full attribute set.

Add `--config .ocmconfig` to the commands in the following steps, or drop the
file in one of the [well-known locations]({{< relref "docs/reference/ocm-cli/ocm.md" >}}) the CLI reads automatically.

{{< /step >}}

{{< step >}}

### Create the component constructor

Each tab describes the same project. Write one of them to `component-constructor.yaml`:

{{< tabs "pypi-spec" >}}
{{< tab "All files of the version" >}}

```bash
cat > component-constructor.yaml << 'EOF'
components:
  - name: github.com/acme.org/myapp
    version: 1.0.0
    provider:
      name: acme.org
    resources:
      - name: requests
        type: pythonPackage
        version: 2.32.3
        relation: external
        access:
          type: pypi/v1alpha1
          indexUrl: https://pypi.org/simple
          project: requests
          version: 2.32.3
EOF
```

{{< /tab >}}
{{< tab "Wheel only" >}}

```bash
cat > component-constructor.yaml << 'EOF'
components:
  - name: github.com/acme.org/myapp
    version: 1.0.0
    provider:
      name: acme.org
    resources:
      - name: requests
        type: pythonPackage
        version: 2.32.3
        relation: external
        access:
          type: pypi/v1alpha1
          indexUrl: https://pypi.org/simple
          project: requests
          version: 2.32.3
          distributions:
            - kind: wheel
EOF
```

{{< callout context="note" >}}
Omitting `distributions` takes every non-yanked file of the version. A
`distributions` entry narrows the set, either by `kind` (`sdist`/`wheel`) or by
an exact `filename`.
{{< /callout >}}

{{< /tab >}}
{{< /tabs >}}

{{< /step >}}

{{< step >}}

### Build the component version

```bash
ocm add cv
```

OCM resolves the project page, downloads the selected files to hash them, and
lists the component:

```text
 COMPONENT                 │ VERSION │ PROVIDER
───────────────────────────┼─────────┼──────────
 github.com/acme.org/myapp │ 1.0.0   │ acme.org
```

{{< /step >}}

{{< step >}}

### Check what ended up in the descriptor

```bash
ocm get cv ./transport-archive//github.com/acme.org/myapp:1.0.0 -o yaml
```

```yaml
    resources:
    - access:
        indexUrl: https://pypi.org/simple
        project: requests
        version: 2.32.3
        type: pypi/v1alpha1
      digest:
        hashAlgorithm: SHA-256
        normalisationAlgorithm: genericBlobDigest/v1
        value: <sha256-over-the-archive>
      name: requests
      relation: external
      type: pythonPackage
      version: 2.32.3
```

The access is recorded as written, and the resource gained a digest over the
downloaded archive. A PyPI release version is immutable, so this digest stays
stable.

{{< /step >}}

{{< step >}}

### Download the resource back

```bash
ocm download resource ./transport-archive//github.com/acme.org/myapp:1.0.0 \
  --identity name=requests \
  --output ./requests.tar.gz
```

The output is a gzipped tar archive holding the distribution files, each
followed by its detached `.asc` signature when the index publishes one. Unpack
it with `tar -xzf`.

{{< /step >}}

{{< /steps >}}

## Next Steps

- [How-To: Download Resources from Component Versions]({{< relref "docs/how-to/download-resources-from-component-versions.md" >}}) -
  Fetch the resource you just added
- [How-To: Air-Gap Transfer]({{< relref "docs/how-to/air-gap-transfer.md" >}}) - Move component versions into
  disconnected environments
- [How-To: Sign a Component Version]({{< relref "sign-component-version.md" >}}) - Cover the digest you just recorded with
  a signature

## Related Documentation

- [Reference: Input and Access Types]({{< relref "docs/reference/input-and-access-types.md#pypiv1alpha1-access" >}}) - Field
  reference for the `PyPI/v1alpha1` access type
- [Reference: Resource Repositories]({{< relref "docs/reference/resource-repositories.md#pypi-resource-repository" >}}) -
  Capabilities, download, upload and digest processing
- [Reference: Credential Consumer Identities]({{< relref "docs/reference/credential-consumer-identities.md#pypirepository" >}}) -
  Identity attributes and matching rules for `PyPIRepository` consumers
- [Reference: Credential Types]({{< relref "docs/reference/credential-types.md#pypicredentialsv1" >}}) - Full field
  reference for `PyPICredentials/v1`
