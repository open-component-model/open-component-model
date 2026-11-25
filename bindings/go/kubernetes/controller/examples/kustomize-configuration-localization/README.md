# kustomize-configuration-localization

Deploys podinfo via a component version that ships its kustomize manifests as
a Flux-consumable OCI artifact embedded in the component, plus the podinfo
container image as a separate resource. The RGD wires both into a Flux
`OCIRepository` + `Kustomization` (and an ArgoCD `Application`) and patches
the image reference and `PODINFO_UI_MESSAGE` env var.

## How the kustomize artifact is built

Install ORAS and follow the [shared prerequisites](../README.md#prerequisites). From this directory,
`task prepare` builds and transfers the component using `transfer.ocmconfig`; then follow the
[kro/Crossplane deployment flow](../README.md#deploying-kro-and-crossplane-examples).

The `kustomize-resource` uses a `dir/v1` input pointing at `./oci-layout` with media type
`application/vnd.ocm.software.oci.layout.v1+tar`. The recipe stages that layout and its companion file inputs
in a fresh temporary directory, leaving any existing `./oci-layout` untouched. To execute the same sequence
directly instead of `task prepare`, run from this directory with the absolute `OCM_CLI` and `IMAGE_REGISTRY`
set by the shared prerequisites:

```bash
set -eu
command -v oras >/dev/null 2>&1 || { echo "oras is required for kustomize-configuration-localization" >&2; exit 1; }
work=$(mktemp -d)
trap 'rm -r -- "$work"' EXIT
cp ./component-constructor.yaml "$work/component-constructor.yaml"
cp ./rgd.yaml ./crossplane-composition.yaml "$work/"
COPYFILE_DISABLE=1 tar -czf "$work/kustomize.tar.gz" -C ./kustomize .
(
  cd "$work"
  oras push --oci-layout ./oci-layout:latest \
    --artifact-type application/vnd.cncf.flux.config.v1+json \
    kustomize.tar.gz:application/vnd.cncf.flux.content.v1.tar+gzip
  "$OCM_CLI" add cv --repository "$work/ctf" --constructor ./component-constructor.yaml
)
"$OCM_CLI" transfer cv "ctf::$work/ctf//ocm.software/ocm-k8s-toolkit/examples/kustomize-configuration-localization" \
  "$IMAGE_REGISTRY" --config ./transfer.ocmconfig
```

The layer's `application/vnd.cncf.flux.content.v1.tar+gzip` media type is what Flux and ArgoCD expect.
ArgoCD's repo-server allowlist must include it; the shared local setup configures that prerequisite.

`ocm add cv` recognizes the layout media type and stores the layout as a local blob. The shipped
`transfer.ocmconfig` selects the OCI uploader first, then the local-blob uploader as a catch-all, so native OCI
artifacts and all remaining resources reach the target registry. It also contains the local registry's
plain-HTTP settings. No implicit `.ocmconfig` file or deprecated `--copy-resources` flag is needed.

Without the OCI uploader, the OCI artifact does not exist at the reference the RGD expects. After transfer,
`resource.access.toOCI()` exposes `registry`, `repository`, and `digest` fields that `rgd.yaml` reads into
the `OCIRepository` and ArgoCD `Application`.

## Editing the kustomize manifests

Edit the files under `./kustomize/` and rerun `task prepare` to publish a fresh artifact. The recipe's temporary
layout is removed on exit; do not commit a manually built layout.

References:

- [Working with OCI tutorial](https://ocm.software/docs/tutorials/working-with-oci/), *Embed an OCI Image Layout* tab
- [Input and Access Types reference](https://ocm.software/docs/reference/input-and-access-types/)
