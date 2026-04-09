---
title : "OCM Components"
description: "Learn about OCM components and their elements."
icon: "🔩"
weight: 31
toc: true
---

In OCM, *components* group a set of semantically related *component versions*. Each component version is uniquely and globally identified by a [*component identity*]({{< relref "coordinates.md" >}}) and can reference other components. A component version can also contain *artifacts* and a formal description of how to access them. These artifacts come in two categories: *resources*, which describe the payload (e.g., OCI images), and *sources*, which describe the input for creating resources (e.g., source code).

For a detailed exploration of OCM components and other key elements of the Open Component Model, please refer to the [OCM Specification](https://github.com/open-component-model/ocm-spec/blob/main/README.md) and its [Glossary](https://github.com/open-component-model/ocm-spec/blob/main/doc/glossary.md).
