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

The primary purpose of OCM K8s Toolkit is simple: Deploy an OCM resource from an OCM component version into a Kubernetes
cluster.

The implementation, however, is a bit more complex as deployments must be secure and configurable. Additionally, an
OCM Resource can, in theory, contain any form of deployable resource, for instance a Helm chart, a Kustomization, or
plain Kubernetes manifests. Each of these resources has its own way of being deployed or
configured. So, instead of creating a generic deployer that offers all these functionalities, we decided to use existing
tools that are already available in the Kubernetes ecosystem.

The following diagram describes a basic scenario in which an OCM resource containing a Helm chart is deployed into a
Kubernetes cluster using the OCM K8s Toolkit as well as kro and FluxCD.
kro is used to orchestrate the deployment and to transport information about the location of the OCM resource to FluxCD.
FluxCD takes the location of the OCM resource, downloads the chart, configures it if necessary,
and deploys it into the Kubernetes cluster.

```mermaid
flowchart TB
    classDef cluster fill:white,color:black,stroke:black;
    classDef reconciledBy fill:#dedede,stroke:black,stroke-dasharray: 5,color:black;
    classDef k8sObject fill:#b3b3b3,color:black,stroke:black;
    classDef information fill:#b3b3b3,color:black,stroke:black,stroke-dasharray: 2;
    classDef ocm fill:white,stroke:black,color:black;
    classDef legendStyle fill:white,stroke:black,color:black,stroke-dasharray: 2;
    classDef legendStartEnd height:0px;
    classDef legendItems fill:#b3b3b3,stroke:none,color:black;

    subgraph legend[Legend]
        start1[ ] ---references[referenced by] --> end1[ ]
        start2[ ] -.-creates -.-> end2[ ]
        start3[ ] ---instanceOf[instance of] --> end3[ ]
        start4[ ] ~~~reconciledBy[reconciled by] ~~~ end4[ ]
        start5[ ] ~~~k8sObject[k8s object] ~~~ end5[ ]
        start6[ ] ~~~templateOf[template of] ~~~ end6[ ]
    end

    subgraph background[ ]
        direction TB

        subgraph ocmRepo[OCM Repository]
           subgraph ocmCV[OCM Component Version]
               subgraph ocmResource[OCM Resource: HelmChart]
               end
           end
        end

        subgraph k8sCluster[Kubernetes Cluster]
            subgraph kroRGD[kro]
                subgraph rgd[RGD: Simple]
                    direction LR
                    rgdRepository[Repository]
                    rgdComponent[Component]
                    rgdResourceHelm[Resource: HelmChart]
                    rgdSource[FluxCD: OCI Repository]
                    rgdHelmRelease[FluxCD: HelmRelease]
                end
            end
            subgraph kroInstance[kro]
                subgraph instanceSimple[Instance: Simple]
                    subgraph ocmK8sToolkit[OCM K8s Toolkit]
                        k8sRepo[Repository] --> k8sComponent[Component] --> k8sResource[Resource: HelmChart]
                    end
                    subgraph fluxCD[FluxCD]
                        source[OCI Repository] --> helmRelease[HelmRelease]
                    end
                    k8sResource --> source
                end
            end
            kroRGD & instanceSimple --> crdSimple[CRD: Simple]
            helmRelease --> deployment[Deployment: Helm chart]
        end

        ocmRepo --> k8sRepo
    end

    linkStyle default fill:none,stroke:black;
    linkStyle 2,3,16,18 stroke:black,stroke-dasharray: 10;
    linkStyle 4,5,17 stroke:black,stroke-dasharray: 4;

    class start1,end1,start2,end2,start3,end3,start4,end4,start5,end5,start6,end6 legendStartEnd;
    class references,creates,instanceOf legendItems;
    class templateOf,rgdRepository,rgdComponent,rgdResourceHelm,rgdSource,rgdHelmRelease information;
    class reconciledBy,ocmK8sToolkit,fluxCD,kroRGD,kroInstance reconciledBy;
    class k8sObject,rgd,k8sRepo,k8sComponent,k8sResource,source,helmRelease,deployment,crdSimple,instanceSimple k8sObject;
    class ocmRepo,ocmCV,ocmResource ocm;
    class k8sCluster cluster;
    class legend legendStyle;
```

The above diagram shows an OCM resource of type `helmChart`. This resource is part of an OCM component version,
which is located in an OCM repository.

In the `Kubernetes Cluster` we can see several Kubernetes (custom) resources. The `ResourceGraphDefintion`
(`RGD: Simple`) contains the template of all the resources for deploying the Helm chart into the Kubernetes cluster.
kro creates a Custom Resource Definition (CRD) `Simple` based on that `ResourceGraphDefinition`. By creating an instance
of this CRD (`Instance: Simple`), the resources are created and reconciled by the respective controllers:

- `Repository`: Points to the OCM repository and checks if it is reachable by pinging it.
- `Component`: Refers to the `Repository` and downloads and verifies the OCM component version descriptor.
- `Resource`: Points to the `Component`, downloads the OCM component version descriptor from which it gets the location
of the OCM resource. It then downloads the resource to verify its signature (optional) and publishes the location of the
resource in its status.

> [!IMPORTANT]
> With FluxCD, this only works if the OCM resource has an access for which FluxCD has a corresponding Source type (e.g.
> an OCI or a GitHub repository)

As a result, FluxCD can now consume the information of the `Resource` and deploy the Helm chart:

- `OCIRepository`: Watches and downloads the resource from the location provided by the `Resource` status.
- `HelmRelease`: Refers to the `OCIRepository`, lets you configure the Helm chart, and creates the deployment into the
Kubernetes cluster.

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
