# OCM Kubernetes Controller Toolkit

> [!CAUTION]
> This project is in early development and not yet ready for production use.

The OCM Kubernetes Controller Toolkit

- supports the deployment of an OCM component and its resources, like Helm charts or other manifests,
into a Kubernetes cluster with the help of kro and a deployer, e.g. FluxCD.
- provides a controller to transfer OCM components.

## What should I know before I start?

You should be familiar with the following concepts:

- [Open Component Model](https://ocm.software/)
- [Kubernetes](https://kubernetes.io/) ecosystem
- [kro](https://kro.run)
- Kubernetes resource deployer such as [FluxCD](https://fluxcd.io/).
- [Task](https://taskfile.dev/)

## Concept

> [!NOTE]
> The following section provides a high-level overview of the OCM K8s Toolkit and its components regarding the
> deployment of an OCM resource in a very basic scenario.

The primary purpose of OCM K8s Toolkit is simple: Deploy an OCM resource 
from an OCM component version into a Kubernetes cluster.

The diagram below provides an overview of the architecture of the OCM 
K8s Toolkit.

![Architecture of OCM K8s Toolkit](./docs/assets/controller-tam.svg)

## Installation

Take a look at our [installation guide](docs/getting-started/setup.md#install-the-ocm-k8s-toolkit) to get started.

> [!IMPORTANT]
> While the OCM K8s Toolkit technically can be used standalone, it requires kro and a deployer, e.g. FluxCD, to deploy
> an OCM resource into a Kubernetes cluster. The OCM K8s Toolkit deployment, however, does not contain kro or any
> deployer. Please refer to the respective installation guides for these tools:
>
> - [kro](https://kro.run/docs/getting-started/Installation/)
> - [FluxCD](https://fluxcd.io/docs/installation/)

## Getting Started

- [Setup your (test) environment with kind, kro, and FluxCD](docs/getting-started/setup.md)
- [Deploying a Helm chart using a `ResourceGraphDefinition` with FluxCD](docs/getting-started/deploy-helm-chart.md)
- [Deploying a Helm chart using a `ResourceGraphDefinition` inside the OCM component version (bootstrap) with FluxCD](docs/getting-started/deploy-helm-chart-bootstrap.md)
- [Configuring credentials for OCM K8s Toolkit resources to access private OCM repositories](docs/getting-started/credentials.md)

## Contributing

Code contributions, feature requests, bug reports, and help requests are very welcome. Please refer to our
[Contributing Guide](https://github.com/open-component-model/.github/blob/main/CONTRIBUTING.md)
for more information on how to contribute to OCM.

OCM K8s Toolkit follows the [CNCF Code of Conduct](https://github.com/cncf/foundation/blob/main/code-of-conduct.md).
