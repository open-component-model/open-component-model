---
title: "Example of a Component Descriptor"
description: "Example of a component descriptor using public-key-based signing."
weight: 46
toc: true
---

The following is an example of a public-key-based signed component descriptor containing a resource, source and one component reference.

The component is publicly available in the GitHub container registry and can be inspected using the following command:

```shell
ocm componentversion get --repo ghcr.io/phoban01/ocm github.com/weaveworks/weave-gitops -oyaml
```

```yaml
meta:
  # component schema version
  schemaVersion: v2
component:
  # name of the component. Must start with URL-prefix that should be controlled
  # by the owner of the component to avoid collisions
  # regex: ^[a-z][-a-z0-9]*([.][a-z][-a-z0-9]*)*[.][a-z]{2,}(/[a-z][-a-z0-9_]*([.][a-z][-a-z0-9_]*)*)+$
  name: github.com/weaveworks/weave-gitops
  # version of the component. Must adhere to “relaxed SemVer”
  # major, minor (+ optional patch level) - optional v-prefix
  # regex: ^[v]?(0|[1-9]\\d*)(?:\\.(0|[1-9]\\d*))?(?:\\.(0|[1-9]\\d*))?(?:-((?:0|[1-9]\\d*|\\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\\.(?:0|[1-9]\\d*|\\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\\+([0-9a-zA-Z-]+(?:\\.[0-9a-zA-Z-]+)*))?$

  version: v1.0.0
  # component provider
  provider: weaveworks
  # list of labels that can contain arbitrary metadata in form of K/V pairs
  # labels can be added on component root, resource, source and reference level
  labels:
  - name: link-to-documentation
    value: https://github.com/weaveworks/weave-gitops
  # list of repository context the component version "lived" in,
  # with the current one at the top
  repositoryContexts:
  - baseUrl: ghcr.io
    componentNameMapping: urlPath
    subPath: phoban01/ocm
    type: OCIRegistry
  # list of resources that describe the payload of the component
  resources:
    # resource name
  - name: image
    # resource location (external repository or internal to this repository)
    relation: external
    # resource type
    type: ociImage
    # resource version. Must also adhere to “relaxed SemVer” (see `component.versio` above`)
    version: v0.14.1
    # metadata describing how to access the resource
    access:
      # type of access information
      type: ociArtifact
      imageReference: ghcr.io/weaveworks/wego-app:v0.14.1
    # signing metadata for the resource (if component has been signed)
    digest:
      hashAlgorithm: SHA-256
      normalisationAlgorithm: ociArtifactDigest/v1
      value: efa2b9980ca2de65dc5a0c8cc05638b1a4b4ce8f6972dc08d0e805e5563ba5bb
  # list of sources that describe the input for creating the resources
  sources:
    # source name
  - name: weave-gitops
    # source type
    type: git
    # source version. Must also adhere to “relaxed SemVer” (see `component.versio` above`)
    version: v0.14.1
    # metadata describing how to access the source
    access:
      commit: 727513969553bfcc603e1c0ae1a75d79e4132b58
      ref: refs/tags/v0.14.1
      repoUrl: github.com/weaveworks/weave-gitops
      type: gitHub
  # list of references to other components
  componentReferences:
  # reference name
  - name: prometheus
    # reference version
    version: v1.0.0
    # referenced component name
    componentName: cncf.io/prometheus
    # signing metadata for the referenced resource (if component has been signed)
    digest:
      hashAlgorithm: SHA-256
      normalisationAlgorithm: jsonNormalisation/v1
      value: 04eb20b6fd942860325caf7f4415d1acf287a1aabd9e4827719328ba25d6f801
# list of signatures used for signing and verification
signatures:
  # name of the signature
- name: ww-dev
  # digest of the signature including the algorithm used
  digest:
    hashAlgorithm: SHA-256
    normalisationAlgorithm: jsonNormalisation/v1
    value: 4faff7822616305ecd09284d7c3e74a64f2269dcc524a9cdf0db4b592b8cee6a
  # signature including the algorithm used
  signature:
    algorithm: RSASSA-PSS
    mediaType: application/vnd.ocm.signature.rsa
    value: 26468587671bdbd2166cf5f69829f090c10768511b15e804294fcb26e552654316c8f4851ed396f279ec99335e5f4b11cb043feb97f1f9a42115f4fda2d31ae8b481b7303b9a913d3a4b92d446fbee9ed487c93b09e513f3f68355040ec08454675e1f407422062abbd2681f70dd5488ad29020b30cfa7e001455c550458da96166bc3243c8426977d73352aface5323fb2b5a374e9c31b272a59c160b85631231c9fc2f23c032401b80fef937029a39111cee34470c61ae86cd4942553466411a5a116159fdcc10e50fe9360c5184028e72d1fe9c7315f26e15d7b4849f62d197501b8cc6b6f1b1391ecc2fc2fc0c1290d2554594505b25fa8f9bfb28c8df24
```
