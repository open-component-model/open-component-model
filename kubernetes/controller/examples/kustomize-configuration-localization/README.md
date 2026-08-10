# kustomize-configuration-localization

Deploys podinfo via a component version that ships its kustomize manifests as
a Flux-consumable OCI artifact embedded in the component, plus the podinfo
container image as a separate resource. The RGD wires both into a Flux
`OCIRepository` + `Kustomization` (and an ArgoCD `Application`) and patches
the image reference and `PODINFO_UI_MESSAGE` env var.

## How the kustomize artifact is built

The `kustomize-resource` uses a `dir/v1` input pointing at `./oci-layout`
with media type `application/vnd.ocm.software.oci.layout.v1+tar`.
`./oci-layout` is not checked in. Build it from `./kustomize/` with `oras`
before running `ocm add cv`:

```bash
tar -czf kustomize.tar.gz -C ./kustomize .
oras push --oci-layout ./oci-layout:latest \
  --artifact-type application/vnd.cncf.flux.config.v1+json \
  kustomize.tar.gz:application/vnd.cncf.flux.content.v1.tar+gzip
```

The layer's `application/vnd.cncf.flux.content.v1.tar+gzip` media type is
what Flux's `OCIRepository` and ArgoCD expect. ArgoCD additionally requires
this media type in its `ARGOCD_REPO_SERVER_OCI_LAYER_MEDIA_TYPES` allowlist.

`ocm add cv` recognizes the OCM layout media type, unpacks the layout, and
stores the contained manifest as a native OCI artifact. After
`ocm transfer cv`, `resource.access.toOCI()` returns
`<registry>/<subPath>/component-descriptors/<component>@<digest>`, which the
RGD feeds into the `OCIRepository` and an ArgoCD `Application`.

## Editing the kustomize manifests

Edit the files under `./kustomize/`, rebuild `./oci-layout/` afterwards. Do not
commit it.

References:

- [Working with OCI tutorial](https://ocm.software/docs/tutorials/working-with-oci/), *Embed an OCI Image Layout* tab
- [Input and Access Types reference](https://ocm.software/docs/reference/input-and-access-types/)
