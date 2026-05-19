# Open Component Model

> Looking for the original OCM project before our start with the next generation of OCM? Check out the
> [previous repository](https://github.com/open-component-model/ocm)

[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/10065/badge)](https://www.bestpractices.dev/projects/10065)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/open-component-model/open-component-model/badge)](https://scorecard.dev/viewer/?uri=github.com/open-component-model/open-component-model)
[![REUSE status](https://api.reuse.software/badge/github.com/open-component-model/open-component-model)](https://api.reuse.software/info/github.com/open-component-model/open-component-model)
[![CI](https://github.com/open-component-model/open-component-model/actions/workflows/ci.yml/badge.svg)](https://github.com/open-component-model/open-component-model/actions/workflows/ci.yml)

The Open Component Model (OCM) is an open standard to describe software bills of delivery (SBOD). OCM is a
technology-agnostic and machine-readable format focused on the software artifacts that must be delivered for software
products.

Check out the [OCM project web page](https://ocm.software) to find out what OCM offers for implementing a
secure software supply chain. It is the central entry point to all kinds of OCM-related
[docs and guides](https://ocm.software/docs/overview/),
the [OCM specification](https://github.com/open-component-model/ocm-spec/blob/main/README.md), and all project
[GitHub repositories](https://github.com/open-component-model). It also offers a
[Getting Started](https://ocm.software/docs/getting-started/) guide to quickly get your hands dirty with OCM, its
toolset, and concepts.

## OCM Specifications

OCM describes delivery [artifacts](https://github.com/open-component-model/ocm-spec/tree/main/doc/01-model/02-elements-toplevel.md#artifacts-resources-and-sources)
that can be accessed from many types of [component repositories](https://github.com/open-component-model/ocm-spec/tree/main/doc/01-model/01-model.md#component-repositories).
It defines a set of semantic, formatting, and other specifications, all of which can be found in the
[`ocm-spec` repository](https://github.com/open-component-model/ocm-spec). Start learning about
[the core model elements here](https://github.com/open-component-model/ocm-spec/tree/main/doc/01-model/02-elements-toplevel.md#model-elements).

## OCM Library

> **Work In Progress**: This OCM Library is a completely new take on interacting and working with OCM. As such, expect
> heavy changes, especially in the Library API. We are working on a stable API and will release it as soon as possible.
> Until then, please use the library at your own risk.

This project provides a Go library containing an API for interacting with the
[Open Component Model (OCM)](https://github.com/open-component-model/ocm-spec) elements and mechanisms.

The library currently supports the following [repository mappings](https://github.com/open-component-model/ocm-spec/tree/main/doc/03-persistence/02-mappings.md#mappings-for-ocm-persistence):

- **OCI**: Use the repository prefix path of an OCI repository to implement an OCM repository.
- **CTF (Common Transport Format)**: Use a file-based binding to represent any set of component versions as filesystem
  content (directory, tar, tgz).

Additionally, OCM provides a generic solution for how to:

- Sign component versions in any supported OCM repository implementation.
- Verify signatures based on public keys or verified certificates.
- Transport component versions, per reference or as values to any of the repository implementations.

### [OCM CLI](cli)

> **Work In Progress**: This OCM CLI is a completely new take on interacting and working with OCM. As such, expect
> heavy changes, especially in the commands available. We are working on a stable API and will release it as soon as
> possible. Until then, please use the CLI at your own risk.

The [`ocm` CLI](cli/docs/reference/ocm.md) makes it easy to create, sign, verify, and transfer component versions as
well as embed them in build processes. To install the CLI, run:

```bash
curl -sfL https://ocm.software/install-cli.sh | bash
```

For more installation options, see the [installation guide](https://ocm.software/docs/getting-started/install-the-ocm-cli/).

You can also use the OCM CLI container image:

```bash
docker run -t ghcr.io/open-component-model/cli:latest --help
```

See the [guide](https://ocm.software/docs/how-to/how-to-use-the-ocm-cli-container-image/) on using the OCM CLI
container image for more details.

### [OCM Kubernetes Controller Toolkit](kubernetes/controller)

The OCM Kubernetes Controller Toolkit provides a Kubernetes operator to deploy OCM resources into a Kubernetes
cluster. You can install the operator using the provided [Helm chart](kubernetes/controller/chart):

```bash
helm install ocm-k8s-toolkit oci://ghcr.io/open-component-model/kubernetes/controller/chart \
    --namespace ocm-k8s-toolkit-system \
    --create-namespace
```

While the OCM Kubernetes Controller Toolkit can be used standalone to deploy manifests from OCM resources, its full
potential is unlocked
when combined with [kro](https://kro.run/docs/getting-started/Installation/) and a deployer such as
[FluxCD](https://fluxcd.io/docs/installation/). Together, they enable deploying Helm charts or Kustomizations from OCM
resources, including configuration and localization at deploy time.
Check out the [guides](https://ocm.software/docs/getting-started/) for more details.

### [OCM Language Bindings](bindings)

Language bindings are provided for:

- [Go](bindings/go). These bindings are also used by the OCM CLI and are our primary focus.
- Looking for usage examples? Check out the [tested examples](bindings/go/examples/) covering blobs, descriptors,
  credentials, signing, and repository operations.

> We are open to discussing and implementing bindings for other languages. If you are interested in a specific language,
> please open an issue or contact us directly. Contributions are always welcome!

## Contributing

Code contributions, feature requests, bug reports, and help requests are very welcome. Please refer to the
[Contributing Guide in the Community repository](https://github.com/open-component-model/.github/blob/main/CONTRIBUTING.md)
for more information on how to contribute to OCM.

OCM follows the [NeoNephos Code of Conduct](https://github.com/neonephos/.github/blob/main/CODE_OF_CONDUCT.md).

## Licensing

Please see our [LICENSE](LICENSE) for copyright and license information.
Detailed information including third-party components and their licensing/copyright information is available
[via the REUSE tool](https://api.reuse.software/info/github.com/open-component-model/open-component-model).

---

<p align="center"><img alt="Bundesministerium für Wirtschaft und Energie (BMWE)-EU funding logo" src="https://apeirora.eu/assets/img/BMWK-EU.png" width="400"/></p>
