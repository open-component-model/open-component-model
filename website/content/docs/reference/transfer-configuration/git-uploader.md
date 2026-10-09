---
title: "Git Uploader"
description: "Reference for git.uploader.transfer.config.ocm.software/v1alpha1: push matched Git resources with their history into an existing Git repository."
weight: 9
toc: true
---

Pushes a selected Git resource into an existing Git repository. The commits are
copied unchanged: the commit SHA, authorship, parents and signatures stay the
same, and no commit is created. The resource is published with a `Git/v1`
access on the target repository, the full ref and the commit, which downloads to
the same digest.

## Schema

{{< schema-renderer url="/schemas/bindings/go/transfer/GitUploaderConfig.schema.json" >}}

## Fields

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `match` | CEL expression | no | A CEL boolean expression selecting the resources this uploader handles (`resource`, `component` and `target` are available; test access types with `resource.access.isType`). When omitted, the default below applies. |
| `repository` | CEL expression | one of `repository`, `baseUrl` | URL of the existing target repository in any form a `Git/v1` access accepts: a `${…}` CEL template or a plain literal. |
| `baseUrl` | string | one of `repository`, `baseUrl` | URL below which the target repository is addressed by the repository path of a `Git` access's origin: `https://git.example.com` turns `https://github.com/org/repo.git` into `https://git.example.com/org/repo.git`. Needs a `Git` access; a local blob needs an explicit `repository`. |
| `ref` | CEL expression | no | Full branch or tag ref the commit is pushed to, e.g. `refs/heads/main` or `refs/tags/v1.0.0`. Defaults to the ref of a `Git` access (see [Refs](#refs)); a local blob needs an explicit `ref`. |

`repository` and `ref` are evaluated while the transfer graph is built, so a
template that does not evaluate, an invalid repository URL, or a ref that is not
a branch or tag fails the transfer before anything is pushed.

## Default `match`

```yaml
match: resource.access.isType("Git")
```

The default does not depend on the transfer target: a configured Git uploader
pushes Git resources, whether the component version goes to an OCI registry or
a CTF archive. A Git resource copied by value becomes a local blob that no longer
carries its origin, so it is not selected by default (see [Air-gapped
transfer](#air-gapped-transfer)).

## Origin

The origin of a `Git` access is the repository path of its source (without
scheme, host and port) and its ref. `resource.access.toGit()` returns it as a map
with `repository` and `ref`:

| Access | `toGit().repository` | `toGit().ref` |
| --- | --- | --- |
| `Git` `https://github.com/org/repo.git`, ref `refs/heads/main` | `org/repo.git` | `refs/heads/main` |

The defaults use the origin:

```yaml
ref: ${resource.access.toGit().ref}
# with baseUrl: https://git.example.com
repository: ${"https://git.example.com/" + resource.access.toGit().repository}
```

Copying a Git resource as a local blob keeps the archive and the resource digest
unchanged and marks it with the media type
`application/vnd.ocm.software.git.archive.v1+tar+gzip`, but does not record the
origin. `toGit()` and the defaults need a `Git` access; a local blob is pushed
with an explicit `repository` and `ref` (see [Air-gapped
transfer](#air-gapped-transfer)).

## Refs

The target ref must be a full branch or tag ref. Constructing a component
version records the ref of a `Git` access by its full name, so the
origin says whether it was a branch or a tag: a short name becomes the branch
it matched or, failing that, the tag (`ref: v1.0.0` is recorded as
`refs/tags/v1.0.0`), and `HEAD` becomes the branch it pointed to. The archive and
the resource digest do not contain the ref, and signing normalization excludes
the access.

Component versions constructed before full refs were recorded can carry a short
name such as `main`, which does not say whether it is a branch or a tag. The
transfer then fails and asks for an explicit `ref`, e.g. `ref: refs/heads/main`.
`HEAD`, an empty ref and refs other than branches and tags fail the same way.

The ref is then pointed at the commit:

- A branch is created, or fast-forwarded when the commit descends from its
  current tip. A branch that does not fast-forward to the commit fails the transfer.
- A tag is created as a lightweight tag. A tag at another commit fails the transfer.
- A ref that already points at the commit is left unchanged, so repeated
  transfers succeed.

The full history of the commit is pushed, so the target repository may be empty.
A `Git` access must be pinned to a `commit`. The resource digest, if set, is
verified against the archive.

## Air-gapped transfer

On the connected side, copy Git resources into a CTF archive with a local blob
uploader:

```yaml
- type: localblob.uploader.transfer.config.ocm.software/v1alpha1
```

The archive keeps its history and is marked with the media type
`application/vnd.ocm.software.git.archive.v1+tar+gzip`, but it does not carry its
origin. On the air-gapped side, select it explicitly and set `repository` and
`ref` (`baseUrl` and the ref default need a `Git` access):

```yaml
- type: git.uploader.transfer.config.ocm.software/v1alpha1
  match: resource.access.isType("LocalBlob") && has(resource.access.mediaType) && resource.access.mediaType == "application/vnd.ocm.software.git.archive.v1+tar+gzip"
  repository: https://git.internal.example/mirror/app.git
  ref: refs/heads/main
```

Local blobs constructed from a Git input have the media type `application/x-tgz`;
match them the same way. Local blobs of resources constructed with OCM v1 hold the
files only and are rejected.

## Examples

Push one resource to a tag named after the component version:

```yaml
- type: git.uploader.transfer.config.ocm.software/v1alpha1
  match: resource.name == "sources"
  repository: https://git.internal.example/mirror/app.git
  ref: '${"refs/tags/v" + component.version}'
```

Credentials for the target are resolved for the `Git` consumer identity of the
target repository URL, as for downloads.
