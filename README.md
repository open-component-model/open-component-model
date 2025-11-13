# Open Component Model

> Looking for the original OCM project before our start with the next Generation of OCM? Check out the [previous repository](https://github.com/open-component-model/ocm)

[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/10065/badge)](https://www.bestpractices.dev/projects/10065)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/open-component-model/open-component-model/badge)](https://scorecard.dev/viewer/?uri=github.com/open-component-model/open-component-model)
[![REUSE status](https://api.reuse.software/badge/github.com/open-component-model/open-component-model)](https://api.reuse.software/info/github.com/open-component-model/open-component-model)
[![CI](https://github.com/open-component-model/open-component-model/actions/workflows/ci.yml/badge.svg)](https://github.com/open-component-model/open-component-model/actions/workflows/ci.yml)

The Open Component Model (OCM) is an open standard to describe software bills of delivery (SBOD). OCM is a technology-agnostic and machine-readable format focused on the software artifacts that must be delivered for software products.

Check out the [the main OCM project web page](https://ocm.software) to find out what OCM offers you for implementing a secure software supply chain. It is your central entry point to all kind of OCM related [docs and guides](https://ocm.software/docs/overview/about), the [OCM specification](https://github.com/open-component-model/ocm-spec/blob/main/README.md) and all project [github repositories](https://github.com/open-component-model). It also offers a [Getting Started](https://ocm.software/docs/getting-started/) to quickly make your hands dirty with OCM, its toolset and concepts :smiley:

## OCM Specifications

OCM describes delivery [artifacts](https://github.com/open-component-model/ocm-spec/tree/main/doc/01-model/02-elements-toplevel.md#artifacts-resources-and-sources) that can be accessed from many types of [component repositories](https://github.com/open-component-model/ocm-spec/tree/main/doc/01-model/01-model.md#component-repositories). It defines a set of semantic, formatting, and other types of specifications that can be found in the [`ocm-spec` repository](https://github.com/open-component-model/ocm-spec). Start learning about [the core concepts of OCM elements here](https://github.com/open-component-model/ocm-spec/tree/main/doc/01-model/02-elements-toplevel.md#model-elements).

## OCM Library

> **Work In Progress**: This OCM Library is a completely new take on interacting and working with OCM. As such, expect heavy changes, especially in the Library API. We are working on a stable API and will release it as soon as possible. Until then, please use the library at your own risk and [reference the previous implementation here](https://github.com/open-component-model/ocm)

This project provides a Go library containing an API for interacting with the
[Open Component Model (OCM)](https://github.com/open-component-model/ocm-spec) elements and mechanisms.

The library currently supports the following [repository mappings](https://github.com/open-component-model/ocm-spec/tree/main/doc/03-persistence/02-mappings.md#mappings-for-ocm-persistence):

- **OCI**: Use the repository prefix path of an OCI repository to implement an OCM
  repository.
- **CTF (Common Transport Format)**: Use a file-based binding to represent any set of
  component versions as filesystem content (directory, tar, tgz).

Additionally, OCM provides a generic solution for how to:

- Sign component versions in any supported OCM repository implementation.
- Verify signatures based on public keys or verified certificates.
- Transport component versions, per reference or as values to any of the
  repository implementations.

## [OCM CLI](cli/docs/reference/ocm.md)

> **Work In Progress**: This OCM CLI is a completely new take on interacting and working with OCM. As such, expect heavy changes, especially in the Commands available. We are working on a stable API and will release it as soon as possible. Until then, please use the library at your own risk and [reference the previous implementation here](https://github.com/open-component-model/ocm)

The [`ocm` CLI](cli/docs/reference/ocm.md) may also be used to interact with OCM mechanisms. It makes it easy to create component versions and embed them in build processes.

The code for the CLI can be found in [`cli`](cli).

## [OCM Language Bindings](bindings)

We supply language bindings for:

- [go](bindings/go). These Bindings are also used by the OCM CLI and are our primary Focus.

> We are open to discussing and implementing bindings for other languages. If you are interested in a specific language, please open an issue or contact us directly. Contributions are always welcome!

## Contributing

Code contributions, feature requests, bug reports, and help requests are very welcome. Please refer to the [Contributing Guide in the Community repository](https://github.com/open-component-model/.github/blob/main/CONTRIBUTING.md) for more information on how to contribute to OCM.

OCM follows the [CNCF Code of Conduct](https://github.com/cncf/foundation/blob/main/code-of-conduct.md).

## Licensing

Please see our [LICENSE](LICENSE) for copyright and license information.
Detailed information including third-party components and their licensing/copyright information is available [via the REUSE tool](https://api.reuse.software/info/github.com/open-component-model/open-component-model).
