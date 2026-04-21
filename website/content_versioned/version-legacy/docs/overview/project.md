---
title : "About the OCM Project"
description: "Learn about the Open Component Model (OCM) project and its main components."
icon: "💡"
weight: 11
toc: true
images: []
---

The Open Component Model (OCM) is an open standard that enables teams to describe software artifacts and their lifecycle metadata in a consistent,
technology-agnostic way. It’s built to support secure, reliable delivery and deployment of software—across cloud, on-prem, hybrid, and even air-gapped environments.
By decoupling and streamlining lifecycle processes like compliance checks, security scans, and deployments, OCM helps teams collaborate more effectively
and build trust into every step of the software supply chain.

![OCM use cases](./ocm-uses-cases.png)

Below are the main projects of our [Github org](https://github.com/open-component-model). This version of the website references the released version of the OCM library and CLI (aka v1).
The link to the OCM controllers already points to our new Kubernetes controller implementation based on the new OCM library v2
in our new monorepo [open-component-model](https://github.com/open-component-model/open-component-model/).

- [OCM Specification](https://github.com/open-component-model/ocm-spec/blob/main/README.md) - The `ocm-spec` repository contains the OCM specification, which provides a formal description of OCM and its format to describe software artifacts and a storage layer to persist those and make them accessible from remote.
- [OCM Core Library](https://github.com/open-component-model/ocm#ocm-library) - The `ocm` core library contains an API for interacting with OCM elements. A guided tour on how to work with the library can be found [here](https://github.com/open-component-model/ocm/tree/main/examples/lib/tour#readme).
- [OCM CLI](https://github.com/open-component-model/ocm#ocm-cli) - With the `ocm` command line interface end users can interact with OCM elements, helping them create component versions and embed them in CI and CD processes.
- [OCM Controllers](https://github.com/open-component-model/open-component-model/blob/main/kubernetes/controller/README.md) - The controllers are designed to enable the automated deployment of software using OCM, [kro](https://kro.run) and a deployer like [Flux](https://fluxcd.io).
- [OCM Website](https://github.com/open-component-model/ocm-website) - The `ocm-website` you are currently visiting. It is built using Hugo and hosted on Github Pages.
