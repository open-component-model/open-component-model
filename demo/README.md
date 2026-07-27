# Cosign-signable OCM components (normalized layout) — end-to-end demo

**What this proves:** an OCM component published in the *normalized* layout has a stable OCI
manifest digest that does **not** change when the component is copied between registries.
So you can `cosign sign` it exactly like a Docker image, and the signature stays valid after
`ocm transfer` moves the component across registries — here `zot1 → CTF → zot2`, verified on zot2
without re-signing.

Why it works: the normalized manifest is *access-free* and carries **no** per-registry annotations
(no `creator`/`authors`), and its single layer is the JCS-canonical descriptor. Re-materializing the
component on another registry therefore yields a **bit-identical** manifest digest. The default
(`v2`) layout embeds `software.ocm.creator` in the manifest, so its digest changes per registry and
a signature would not survive — which is exactly why the normalized layout exists.

## Prerequisites

Put these tools on your `PATH`:

- `docker` — runs the two local registries
- `cosign` (v3.x) — signs and verifies the component
- `curl` and `jq` — inspect the registry in the walkthrough
- `skopeo` — optional, for the manifest-digest inspections
- `go` — only needed to build the `ocm` CLI from source (next step)

Build the `ocm` CLI from this checkout. The bindings live in sibling modules, so a Go workspace
file lets the CLI build against the in-repo code:

```bash
# from the repo root
go work init ./cli ./bindings/go/oci ./bindings/go/transfer 2>/dev/null \
  || go work use ./cli ./bindings/go/oci ./bindings/go/transfer
(cd cli && go build -o ./tmp/ocm .)
OCM=./cli/tmp/ocm
```

The `--layout` flag is added by this change, so the demo requires the `ocm` binary built above.

## 1. Start two local registries (zot1, zot2)

```bash
docker run -d --rm --name zot1 -p 5001:5000 -v "$PWD/demo/zot-config.json:/etc/zot/config.json" ghcr.io/project-zot/zot-linux-amd64:latest
docker run -d --rm --name zot2 -p 5002:5000 -v "$PWD/demo/zot-config.json:/etc/zot/config.json" ghcr.io/project-zot/zot-linux-amd64:latest
curl -sf http://localhost:5001/v2/ && echo " zot1 up"
curl -sf http://localhost:5002/v2/ && echo " zot2 up"
```

## 2. Publish the component in the normalized layout to zot1

`demo/component-constructor.yaml` defines a component with one local (`utf8`) resource.

```bash
$OCM add component-version --layout normalized -r oci::http://localhost:5001/ocm -c demo/component-constructor.yaml
```

The tag `1.0.0` now resolves to the normalized manifest — `artifactType`
`application/vnd.ocm.software.component-descriptor.normalized.v1`, an empty OCI config, one
normalized-descriptor layer, and layout annotations, with **no** creator/author fields:

```console
$ curl -s http://localhost:5001/v2/ocm/component-descriptors/ocm.software/demo/cosign-normalized/manifests/1.0.0 \
    -H 'Accept: application/vnd.oci.image.manifest.v1+json' | jq '{artifactType, annotations}'
{
  "artifactType": "application/vnd.ocm.software.component-descriptor.normalized.v1",
  "annotations": {
    "org.opencontainers.image.title": "OCM Normalized Component Descriptor for ocm.software/demo/cosign-normalized in version 1.0.0",
    "org.opencontainers.image.version": "1.0.0",
    "software.ocm.component-model/layout-version": "v1",
    "software.ocm.component-model/normalisation-algo": "jsonNormalisation/v4alpha1"
  }
}
```

Its digest — the thing cosign will sign — is stable:

```console
$ curl -sI http://localhost:5001/v2/ocm/component-descriptors/ocm.software/demo/cosign-normalized/manifests/1.0.0 \
    -H 'Accept: application/vnd.oci.image.manifest.v1+json' | grep -i docker-content-digest
Docker-Content-Digest: sha256:133c3c751f0d3dc699d33de182f7cefe960a721b9e08ad01f29dd510e32ef795
```

(The access-bearing descriptor + local blobs live in an `access.v1` referrer of this manifest,
discoverable via the referrers API and a `sha256-<manifest-digest>.acc` fallback tag.)

## 3. Sign it with cosign — exactly like a Docker image

```bash
cd demo
export COSIGN_PASSWORD=""
cosign generate-key-pair
REF=localhost:5001/ocm/component-descriptors/ocm.software/demo/cosign-normalized:1.0.0
cosign sign   --key cosign.key --use-signing-config=false --tlog-upload=false --allow-insecure-registry --allow-http-registry --yes $REF
cosign verify --key cosign.pub --insecure-ignore-tlog=true --allow-insecure-registry --allow-http-registry $REF
cd ..
```

`cosign sign` targets the tag like any image (it resolves to the normalized manifest digest) and
attaches the signature as an **OCI referrer** of that manifest. `cosign verify` on zot1 succeeds; the
payload binds `docker-manifest-digest: sha256:133c3c75…` (the normalized manifest digest).

Flag notes for a local, offline registry with cosign 3.x:
`--use-signing-config=false --tlog-upload=false` (no transparency log), `--allow-insecure-registry
--allow-http-registry` (plain-HTTP zot), `--insecure-ignore-tlog=true` on verify. Use a key pair to
stay offline; keyless/Fulcio is a drop-in variation with a reachable Sigstore.

Both referrers of the normalized manifest are now present on zot1:

```console
$ curl -s "http://localhost:5001/v2/ocm/component-descriptors/ocm.software/demo/cosign-normalized/referrers/sha256:133c3c751f0d3dc699d33de182f7cefe960a721b9e08ad01f29dd510e32ef795" \
    | jq -r '.manifests[] | "\(.artifactType)  \(.digest)"'
application/vnd.ocm.software.component-descriptor.access.v1   sha256:5107b20d716abb414ed9b0ba97945b3c5247dd4c880520f671735053b50e5271
application/vnd.dev.sigstore.bundle.v0.3+json                 sha256:08736c7f4705f7f84322d99efce8447c7893e92c49dc96d949efc067505e7d4e
```

## 4. Other OCM commands still work on the normalized component

```console
$ $OCM get component-version oci::http://localhost:5001/ocm//ocm.software/demo/cosign-normalized:1.0.0 -o yaml
- component:
    name: ocm.software/demo/cosign-normalized
    provider: ocm.software
    resources:
    - access: { localReference: sha256:9355..., mediaType: text/plain, type: LocalBlob/v1 }
      digest: { hashAlgorithm: SHA-256, normalisationAlgorithm: genericBlobDigest/v1, value: 9355... }
      name: greeting
      type: plainText
      version: 1.0.0
    version: 1.0.0
  meta: { schemaVersion: v2 }

$ $OCM download resource oci::http://localhost:5001/ocm//ocm.software/demo/cosign-normalized:1.0.0 \
      --identity name=greeting --output demo/tmp/greeting.txt
level=INFO msg="resource downloaded successfully" output=demo/tmp/greeting.txt
$ cat demo/tmp/greeting.txt
hello from the normalized, cosign-signed component
```

`get` resolves the descriptor via the signed normalized manifest + its access referrer (with the
bind check), and `download resource` reads the local blob from the access referrer.

## 5. Transfer zot1 → CTF → zot2 (carries the signature)

```bash
$OCM transfer component-version --layout normalized \
    oci::http://localhost:5001/ocm//ocm.software/demo/cosign-normalized:1.0.0 \
    ctf::./demo/transport
$OCM transfer component-version --layout normalized \
    ctf::./demo/transport//ocm.software/demo/cosign-normalized:1.0.0 \
    oci::http://localhost:5002/ocm
```

`ocm transfer` re-materializes the component on each hop. Because the normalized manifest is
deterministic, its digest on zot2 is **identical** to zot1, and transfer carries the component
manifest's non-access referrers (the cosign signature) along:

```console
$ curl -sI http://localhost:5002/v2/ocm/component-descriptors/ocm.software/demo/cosign-normalized/manifests/1.0.0 \
    -H 'Accept: application/vnd.oci.image.manifest.v1+json' | grep -i docker-content-digest
Docker-Content-Digest: sha256:133c3c751f0d3dc699d33de182f7cefe960a721b9e08ad01f29dd510e32ef795   # same as zot1

$ curl -s "http://localhost:5002/v2/ocm/component-descriptors/ocm.software/demo/cosign-normalized/referrers/sha256:133c3c751f0d3dc699d33de182f7cefe960a721b9e08ad01f29dd510e32ef795" \
    | jq -r '.manifests[] | "\(.artifactType)  \(.digest)"'
application/vnd.ocm.software.component-descriptor.access.v1   sha256:5107b20d716abb414ed9b0ba97945b3c5247dd4c880520f671735053b50e5271
application/vnd.dev.sigstore.bundle.v0.3+json                 sha256:08736c7f4705f7f84322d99efce8447c7893e92c49dc96d949efc067505e7d4e   # the SAME signature digest
```

## 6. Verify on zot2 — the proof

```console
$ cosign verify --key demo/cosign.pub --insecure-ignore-tlog=true --allow-insecure-registry --allow-http-registry \
    localhost:5002/ocm/component-descriptors/ocm.software/demo/cosign-normalized:1.0.0
Verification for localhost:5002/...cosign-normalized:1.0.0 --
  - The cosign claims were validated
  - The signatures were verified against the specified public key
[{"critical":{...,"image":{"docker-manifest-digest":"sha256:133c3c751f0d3dc699d33de182f7cefe960a721b9e08ad01f29dd510e32ef795"},...}}]
```

The signature made on **zot1** verifies on **zot2** with no re-signing and no manual copy — because
the normalized manifest digest survived the transfer and `ocm transfer` carried the signature referrer.

## Contrast: the default (v2) layout

Publishing the same component with the default layout produces a manifest that embeds per-registry
identity, so its digest is **not** stable across registries:

```console
$ $OCM add component-version -r oci::http://localhost:5001/v2demo -c demo/component-constructor.yaml
$ curl -s .../v2demo/.../manifests/1.0.0 -H 'Accept: application/vnd.oci.image.manifest.v1+json' | jq '.annotations'
{
  "org.opencontainers.image.authors": "Builtin OCI Repository Plugin",
  "software.ocm.creator": "Builtin OCI Repository Plugin",
  ...
}
```

Those annotations change the manifest bytes (hence the digest) per repository, so a cosign signature
over a v2 component would break on transfer. The normalized layout removes exactly this.

## Cleanup

```bash
docker rm -f zot1 zot2
rm -rf demo/transport demo/tmp demo/cosign.key demo/cosign.pub
```
