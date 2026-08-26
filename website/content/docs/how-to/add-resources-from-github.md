---
title: "Add Resources from GitHub"
description: "Add the source archive of a GitHub commit to a component version with the GitHub access type."
icon: "🐙"
weight: 18
toc: true
---

## Goal

Add a [GitHub repository archive](https://docs.github.com/en/rest/repos/contents?apiVersion=2026-03-10#download-a-repository-archive-tar)
to a component version, either as a resource or as a source, using the
[`GitHub/v1` access type]({{< relref "docs/reference/input-and-access-types.md#githubv1" >}}).
The access stores the repository URL and a commit; the archive stays on GitHub until something asks for it. 

## You'll end up with

- A component version in a local transport archive, with the GitHub archive declared as a resource or as a source
- A resource pinned to a commit and carrying a digest over the archive at that commit — even if you wrote only a `ref`
- That archive downloaded back out, to confirm the round trip works

**Estimated time:** ~10 minutes

## Prerequisites

- [OCM CLI]({{< relref "docs/getting-started/ocm-cli-installation.md" >}}) installed

This guide reads a public repository, so it runs end-to-end without credentials — start at step 2. Step 1 covers
reaching a private repository, or lifting the anonymous rate limit.

## Steps

{{< steps >}}

{{< step >}}

### Authenticate against a private repository {#authenticate-against-a-private-repository}

*Optional — skip this if the repository is public.*

Anonymous requests are rate-limited per IP, and a private repository answers `404` rather than `401`: GitHub does not
reveal that the repository exists. Both problems are solved with a token.

[Configure credentials]({{< relref "docs/how-to/configure-multiple-credentials.md" >}}) of type
`GitHubCredentials/v1`. OCM matches them by a consumer identity of type `GitHubRepository` derived from `repoUrl`:

```bash
cat > .ocmconfig << 'EOF'
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    consumers:
      - identity:
          type: GitHubRepository
          hostname: github.com
        credentials:
          - type: GitHubCredentials/v1
            token: ghp_your_token
EOF
```

Omitting `path` matches every repository on that host. See
[Credential Consumer Identities: GitHubRepository]({{< relref "docs/reference/credential-consumer-identities.md#githubrepository" >}})
for the full attribute set.

{{< callout context="note" >}}
For GitHub Enterprise, set `hostname` to your host. Any host other than `github.com` is treated as Enterprise
automatically, so the credentials need nothing else; set `apiHostname` on the access only when the REST API lives on a
different host than the repository.
{{< /callout >}}

Add `--config .ocmconfig` to the commands in the following steps, or drop the file in one of the
[well-known locations]({{< relref "docs/reference/ocm-cli/ocm.md" >}}) the CLI reads automatically.

{{< /step >}}

{{< step >}}

### Create the component constructor

Each tab describes the same archive. Write one of them to `component-constructor.yaml`:

{{< tabs "github-spec" >}}
{{< tab "Resource: pinned commit" >}}

```bash
cat > component-constructor.yaml << 'EOF'
components:
  - name: github.com/acme.org/myapp
    version: 1.0.0
    provider:
      name: acme.org
    resources:
      - name: ocm-sources
        type: directoryTree
        version: 1.0.0
        relation: external
        access:
          type: GitHub/v1
          repoUrl: https://github.com/open-component-model/open-component-model
          commit: b4bb4e880aa5c159366db7cc2ae800e1ee14dbda
EOF
```

{{< /tab >}}
{{< tab "Resource: branch or tag" >}}

```bash
cat > component-constructor.yaml << 'EOF'
components:
  - name: github.com/acme.org/myapp
    version: 1.0.0
    provider:
      name: acme.org
    resources:
      - name: ocm-sources
        type: directoryTree
        version: 1.0.0
        relation: external
        access:
          type: GitHub/v1
          repoUrl: https://github.com/open-component-model/open-component-model
          ref: refs/tags/v0.8.0
EOF
```

{{< callout context="note" >}}
The `ref` is resolved to a commit while the component version is built, and that commit is added to the access. The `ref`
itself stays for provenance. Which one to write is a question of what you want pinned at build time: 
a commit you already know, or whatever the branch or tag points at right now.
{{< /callout >}}

{{< /tab >}}
{{< tab "Source" >}}

```bash
cat > component-constructor.yaml << 'EOF'
components:
  - name: github.com/acme.org/myapp
    version: 1.0.0
    provider:
      name: acme.org
    sources:
      - name: ocm-sources
        type: directoryTree
        version: 1.0.0
        access:
          type: GitHub/v1
          repoUrl: https://github.com/open-component-model/open-component-model
          commit: b4bb4e880aa5c159366db7cc2ae800e1ee14dbda
EOF
```

{{< callout context="caution" >}}
A source must carry a `commit`. A `ref` is accepted, but only resources get pinned — nothing rewrites a source, so it
would keep pointing wherever the branch moves to.
{{< /callout >}}

{{< /tab >}}
{{< /tabs >}}

{{< /step >}}

{{< step >}}

### Build the component version

```bash
ocm add cv
```

OCM downloads the archive to hash it, and says so:

```text
level=WARN msg="computing the digest of a github resource downloads the full commit archive and discards it after
hashing" repoUrl=https://github.com/open-component-model/open-component-model commit=b4bb4e88…
```

That is expected: the digest has to be computed over real bytes. The archive is not kept — only its digest is. Then
the component is listed:

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

{{< tabs "github-descriptor" >}}
{{< tab "Resource: pinned commit" >}}

```yaml
    resources:
    - access:
        commit: b4bb4e880aa5c159366db7cc2ae800e1ee14dbda
        repoUrl: https://github.com/open-component-model/open-component-model
        type: GitHub/v1
      digest:
        hashAlgorithm: SHA-256
        normalisationAlgorithm: genericBlobDigest/v1
        value: fc4a80e964482534612b6a02935523b9ecb5160bee91266926a2d1bb027ccfcf
      name: ocm-sources
      relation: external
      type: directoryTree
      version: 1.0.0
```

The access is recorded as written, and the resource gained a digest over the archive at that commit.

{{< /tab >}}
{{< tab "Resource: branch or tag" >}}

```yaml
    resources:
    - access:
        commit: b4bb4e880aa5c159366db7cc2ae800e1ee14dbda
        ref: refs/tags/v0.8.0
        repoUrl: https://github.com/open-component-model/open-component-model
        type: GitHub/v1
      digest:
        hashAlgorithm: SHA-256
        normalisationAlgorithm: genericBlobDigest/v1
        value: fc4a80e964482534612b6a02935523b9ecb5160bee91266926a2d1bb027ccfcf
      name: ocm-sources
      relation: external
      type: directoryTree
      version: 1.0.0
```

The resolved `commit` now sits alongside the `ref`, with the same digest as the pinned variant. The `ref` is kept for
provenance; from here on the `commit` is what OCM reads.

{{< /tab >}}
{{< tab "Source" >}}

```yaml
    sources:
    - access:
        commit: b4bb4e880aa5c159366db7cc2ae800e1ee14dbda
        repoUrl: https://github.com/open-component-model/open-component-model
        type: GitHub/v1
      name: ocm-sources
      type: directoryTree
      version: 1.0.0
```

No `digest` and no `relation`: a source is a pointer, so there is nothing recorded to verify it against later.

{{< /tab >}}
{{< /tabs >}}

{{< /step >}}

{{< step >}}

### Download the resource back

```bash
ocm download resource ./transport-archive//github.com/acme.org/myapp:1.0.0 \
  --identity name=ocm-sources \
  --output ./sources.tar.gz
```

You should see: `level=INFO msg="resource downloaded successfully" output=./sources.tar.gz`.

The output is the gzipped tar archive GitHub serves, written as a single file. Unpack it with `tar -xzf`.

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

- [Reference: Input and Access Types]({{< relref "docs/reference/input-and-access-types.md#githubv1" >}}) - Field
  reference for the `GitHub/v1` access type
- [Reference: Resource Repositories]({{< relref "docs/reference/resource-repositories.md#github-resource-repository" >}}) -
  Capabilities, download and digest processing
- [Reference: Credential Consumer Identities]({{< relref "docs/reference/credential-consumer-identities.md#githubrepository" >}}) -
  Identity attributes and matching rules for `GitHubRepository` consumers
- [Reference: Credential Types]({{< relref "docs/reference/credential-types.md#githubcredentialsv1" >}}) - Full field
  reference for `GitHubCredentials/v1`
