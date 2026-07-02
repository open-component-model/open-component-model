# OCM Kubernetes Controller Toolkit

> [!CAUTION]
> This project is in early development and not yet ready for production use.

The OCM Kubernetes Controller Toolkit

- supports the deployment of an OCM component and its resources, like Helm charts or other manifests,
into a Kubernetes cluster with the help of kro and a deployer, e.g. FluxCD or ArgoCD.
- provides a controller to transfer OCM components.

## What should I know before I start?

You should be familiar with the following concepts:

- [Open Component Model](https://ocm.software/)
- [Kubernetes](https://kubernetes.io/) ecosystem
- [kro](https://kro.run)
- Kubernetes resource deployers such as [FluxCD](https://fluxcd.io/) or [ArgoCD](https://argo-cd.readthedocs.io/).
- [Task](https://taskfile.dev/)

## Concept

> [!NOTE]
> The following section provides a high-level overview of the OCM Kubernetes Controller Toolkit and its components
> regarding the deployment of an OCM resource in a very basic scenario.

The primary purpose of OCM Kubernetes Controller Toolkit is simple: Deploy an OCM resource from an OCM component
version into a Kubernetes cluster.

The diagram below provides an overview of the architecture of the OCM Kubernetes Controller Toolkit.

![Architecture of OCM Kubernetes Controller Toolkit](./docs/assets/controller-tam.svg)

## Installation

Take a look at our [installation guide](https://ocm.software/docs/getting-started/set-up-controller-environments/#install-the-ocm-controllers) to get started.

> [!IMPORTANT]
> While the OCM Kubernetes Controller Toolkit technically can be used standalone, it requires kro and a deployer,
> e.g. FluxCD or ArgoCD, to deploy an OCM resource into a Kubernetes cluster. The OCM Kubernetes Controller Toolkit
> deployment, however, does not contain kro or any deployer. Please refer to the respective installation guides
> for these tools:
>
> - [kro](https://kro.run/docs/getting-started/Installation/)
> - [FluxCD](https://fluxcd.io/docs/installation/)
> - [ArgoCD](https://argo-cd.readthedocs.io/en/stable/getting_started/)

## Getting Started

- [Setup your (test) environment with kind, kro, FluxCD, and ArgoCD](https://ocm.software/docs/getting-started/set-up-controller-environments/)
- [Deploying a Helm chart using a `ResourceGraphDefinition` with FluxCD](https://ocm.software/docs/getting-started/deploy-helm-charts/)
- [Deploying a Helm chart using a `ResourceGraphDefinition` inside the OCM component version (bootstrap) with FluxCD](https://ocm.software/docs/tutorials/deploy-helm-charts-with-bootstrap-setup/)
- [Configuring credentials for OCM Kubernetes Controller Toolkit resources to access private OCM repositories](https://ocm.software/docs/how-to/configure-credentials-for-ocm-controllers/)

## Deployers

The OCM Kubernetes Controller Toolkit is deployer-agnostic: the kro.run `ResourceGraphDefinition` (RGD) you write determines
which tool reconciles the final workload. Both **FluxCD** and **ArgoCD** are supported and tested.

See the working examples under [`examples/`](./examples/) — each `rgd.yaml` contains both a FluxCD and an
ArgoCD deployer block side by side.

## Development

### Running e2e tests

| Task | Description |
|---|---|
| `task test/e2e/setup/local` | Create `ocm-e2e` Kind cluster with kro, FluxCD, and ArgoCD installed |
| `task test/e2e/setup/teardown` | Delete the `ocm-e2e` cluster and its registry container |
| `task test/e2e` | Run e2e tests against the running cluster. Pass ginkgo flags after `--`, e.g. `task test/e2e -- -ginkgo.focus=helm-simple` |
| `task test/e2e/fresh` | Tear down + setup + run in one shot. Accepts the same ginkgo flags |

## Contributing

Code contributions, feature requests, bug reports, and help requests are very welcome. Please refer to our
[Contributing Guide](https://github.com/open-component-model/.github/blob/main/CONTRIBUTING.md)
for more information on how to contribute to OCM.

OCM Kubernetes Controller Toolkit follows the
[Linux Foundation EU Code of Conduct](https://linuxfoundation.eu/policies/code-of-conduct)
