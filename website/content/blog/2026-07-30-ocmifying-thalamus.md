---
title: "Creating an OCM Component for a non-trivial Application"
description: "This post has detailed explanation about how to modernize a non-trivial application as large as Thalamus. Thalamus has over 80 images it needed to localize and a deployment that involved more than 11 components."
date: 2026-07-30T10:00:00+02:00
contributors: []
tags: ["ocm", "thalamus", "component"]
draft: false
---

## The Problem

[Thalamus](https://cobaltcore-dev.github.io/thalamus/main/) is a large project with many moving parts. At the time of
this writing, it has 11 components that must be bundled together so the entire stack can be deployed as one.

It also includes a couple of open source components for which the CRDs and Helm charts also had to be included in the
component version.

Further, there were several requirements that the deployment remains configurable because certain values must overwrite
existing defaults. And on top of that, existing defaults change from environment to environment. This means, there are
three layers of overwrites: global defaults that are true for _every_ environment. Then, environment specific defaults
that you don't want to overwrite every time (for example, prod and dev have different defaults). And lastly, there are
cluster specific values that you would like to have on top of all the defaulting.

This had to be coded together with a very strict deployment ordering.

## What we ended up doing

First, we created a component constructor out of the many dependencies that Thalamus has. All dependencies ended up being
component version references like the below (warning, rather long yaml file incoming):

```yaml
components:
  - name: github.com/cobaltcore-dev/thalamus
    version: 0.1.0
    provider:
      name: cobaltcore-dev
    resources:
      - name: thalamus-chart
        type: helmChart
        version: 0.1.0
        input:
          type: Helm/v1
          path: helm/thalamus
          repository: charts/thalamus:0.1.0
      - name: thalamus-rgd
        type: blob
        version: 0.1.0
        input:
          type: File/v1
          path: ocm-single-component/deploy/rgd.yaml
          mediaType: application/vnd.cncf.kro.resourcegraphdefinition.v1+yaml
      - name: operator-image
        type: ociImage
        version: 0.1.0
        access:
          type: ociArtifact
          imageReference: ghcr.io/cobaltcore-dev/thalamus:latest
      - name: epp-image
        type: ociImage
        version: v0.9.0
        access:
          type: ociArtifact
          imageReference: ghcr.io/llm-d/llm-d-router-endpoint-picker:v0.9.0
    componentReferences:
      - name: thalamus-crds
        componentName: github.com/cobaltcore-dev/thalamus/thalamus-crds
        version: 0.1.0
      - name: gateway-api-crds
        componentName: github.com/cobaltcore-dev/thalamus/gateway-api-crds
        version: 1.5.1
      - name: gateway-api-inference-extension-crds
        componentName: github.com/cobaltcore-dev/thalamus/gateway-api-inference-extension-crds
        version: 1.5.0
      - name: node-feature-discovery
        componentName: github.com/cobaltcore-dev/thalamus/node-feature-discovery
        version: 0.19.0
      - name: gpu-operator
        componentName: github.com/cobaltcore-dev/thalamus/gpu-operator
        version: v26.3.3
      - name: kube-prometheus-stack
        componentName: github.com/cobaltcore-dev/thalamus/kube-prometheus-stack
        version: 87.15.1
      - name: agentgateway-crds
        componentName: github.com/cobaltcore-dev/thalamus/agentgateway-crds
        version: v1.3.1
      - name: agentgateway
        componentName: github.com/cobaltcore-dev/thalamus/agentgateway
        version: v1.3.1
      - name: external-dns
        componentName: github.com/cobaltcore-dev/thalamus/external-dns
        version: 1.19.0
      - name: open-webui
        componentName: github.com/cobaltcore-dev/thalamus/open-webui
        version: 15.2.0

  - name: github.com/cobaltcore-dev/thalamus/thalamus-crds
    version: 0.1.0
    provider:
      name: cobaltcore-dev
    resources:
      - name: thalamus-crds-chart
        type: helmChart
        version: 0.1.0
        input:
          type: Helm/v1
          path: helm/thalamus-crds
          repository: charts/thalamus-crds:0.1.0

  - name: github.com/cobaltcore-dev/thalamus/gateway-api-crds
    version: 1.5.1
    provider:
      name: cobaltcore-dev
    resources:
      - name: gateway-api-chart
        type: helmChart
        version: 1.5.1
        input:
          type: Helm/v1
          path: helm/gateway-api
          repository: charts/gateway-api:1.5.1

  - name: github.com/cobaltcore-dev/thalamus/gateway-api-inference-extension-crds
    version: 1.5.0
    provider:
      name: cobaltcore-dev
    resources:
      - name: gateway-api-inference-extension-chart
        type: helmChart
        version: 1.5.0
        input:
          type: Helm/v1
          path: helm/gateway-api-inference-extension
          repository: charts/gateway-api-inference-extension:1.5.0

  - name: github.com/cobaltcore-dev/thalamus/node-feature-discovery
    version: 0.19.0
    provider:
      name: cobaltcore-dev
    resources:
      - name: node-feature-discovery-chart
        type: helmChart
        version: 0.19.0
        input:
          type: Helm/v1
          helmRepository: oci://registry.k8s.io/nfd/charts/node-feature-discovery:0.19.0
          repository: charts/node-feature-discovery:0.19.0
      - name: node-feature-discovery-image
        type: ociImage
        version: v0.19.0
        access:
          type: ociArtifact
          imageReference: registry.k8s.io/nfd/node-feature-discovery:v0.19.0

  - name: github.com/cobaltcore-dev/thalamus/gpu-operator
    version: v26.3.3
    provider:
      name: cobaltcore-dev
    resources:
      - name: gpu-operator-chart
        type: helmChart
        version: v26.3.3
        input:
          type: Helm/v1
          helmRepository: https://helm.ngc.nvidia.com/nvidia/charts/gpu-operator-v26.3.3.tgz
          repository: charts/gpu-operator:v26.3.3

  - name: github.com/cobaltcore-dev/thalamus/kube-prometheus-stack
    version: 87.15.1
    provider:
      name: cobaltcore-dev
    resources:
      - name: kube-prometheus-stack-chart
        type: helmChart
        version: 87.15.1
        input:
          type: Helm/v1
          helmRepository: oci://ghcr.io/prometheus-community/charts/kube-prometheus-stack:87.15.1
          repository: charts/kube-prometheus-stack:87.15.1
      - name: prometheus-operator-image
        type: ociImage
        version: v0.92.1
        access:
          type: ociArtifact
          imageReference: quay.io/prometheus-operator/prometheus-operator:v0.92.1
      - name: config-reloader-image
        type: ociImage
        version: v0.92.1
        access:
          type: ociArtifact
          imageReference: quay.io/prometheus-operator/prometheus-config-reloader:v0.92.1
      - name: thanos-image
        type: ociImage
        version: v0.42.0
        access:
          type: ociArtifact
          imageReference: quay.io/thanos/thanos:v0.42.0
      - name: prometheus-image
        type: ociImage
        version: v3.13.1-distroless
        access:
          type: ociArtifact
          imageReference: quay.io/prometheus/prometheus:v3.13.1-distroless
      - name: alertmanager-image
        type: ociImage
        version: v0.33.1
        access:
          type: ociArtifact
          imageReference: quay.io/prometheus/alertmanager:v0.33.1
      - name: webhook-certgen-image
        type: ociImage
        version: 1.8.4
        access:
          type: ociArtifact
          imageReference: ghcr.io/jkroepke/kube-webhook-certgen:1.8.4
      - name: kube-state-metrics-image
        type: ociImage
        version: v2.19.1
        access:
          type: ociArtifact
          imageReference: registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.19.1

  - name: github.com/cobaltcore-dev/thalamus/agentgateway-crds
    version: v1.3.1
    provider:
      name: cobaltcore-dev
    resources:
      - name: agentgateway-crds-chart
        type: helmChart
        version: v1.3.1
        input:
          type: Helm/v1
          helmRepository: oci://cr.agentgateway.dev/charts/agentgateway-crds:v1.3.1
          repository: charts/agentgateway-crds:v1.3.1

  - name: github.com/cobaltcore-dev/thalamus/agentgateway
    version: v1.3.1
    provider:
      name: cobaltcore-dev
    resources:
      - name: agentgateway-chart
        type: helmChart
        version: v1.3.1
        input:
          type: Helm/v1
          helmRepository: oci://cr.agentgateway.dev/charts/agentgateway:v1.3.1
          repository: charts/agentgateway:v1.3.1
      - name: controller-image
        type: ociImage
        version: v1.3.1
        access:
          type: ociArtifact
          imageReference: cr.agentgateway.dev/controller:v1.3.1
      - name: proxy-image
        type: ociImage
        version: v1.3.1
        access:
          type: ociArtifact
          imageReference: cr.agentgateway.dev/agentgateway:v1.3.1

  - name: github.com/cobaltcore-dev/thalamus/external-dns
    version: 1.19.0
    provider:
      name: cobaltcore-dev
    resources:
      - name: external-dns-chart
        type: helmChart
        version: 1.19.0
        input:
          type: Helm/v1
          helmRepository: https://github.com/kubernetes-sigs/external-dns/releases/download/external-dns-helm-chart-1.19.0/external-dns-1.19.0.tgz
          repository: charts/external-dns:1.19.0
      - name: external-dns-image
        type: ociImage
        version: v0.19.0
        access:
          type: ociArtifact
          imageReference: registry.k8s.io/external-dns/external-dns:v0.19.0

  - name: github.com/cobaltcore-dev/thalamus/open-webui
    version: 15.2.0
    provider:
      name: cobaltcore-dev
    resources:
      - name: open-webui-chart
        type: helmChart
        version: 15.2.0
        input:
          type: Helm/v1
          helmRepository: https://helm.openwebui.com/open-webui-15.2.0.tgz
          repository: charts/open-webui:15.2.0
      - name: open-webui-image
        type: ociImage
        version: 0.10.2
        access:
          type: ociArtifact
          imageReference: ghcr.io/open-webui/open-webui:0.10.2
      - name: redis-image
        type: ociImage
        version: 7.4.2-alpine3.21
        access:
          type: ociArtifact
          imageReference: docker.io/library/redis:7.4.2-alpine3.21
```

Using the above content in a `component-constructor.yaml`, we simply ran:

```console
ocm add component-version \
  --constructor component-constructor.yaml \
  --repository ghcr.io/source-repository/thalamus
```

Once the component was created properly we transferred it to a new location:

```console
ocm transfer cv \
  ghcr.io/source-repository//github.com/cobaltcore-dev/thalamus:0.1.0 \
  ghcr.io/target-repository \
  --recursive --copy-resources --upload-as ociArtifact
```

The `--upload-as ociArtifact` here was important, because all the images and resources are OCI artifacts and [oras](https://oras.land/)
(OCM's internal OCI handling SDK) is capable of Streaming OCI -> OCI content efficiently. Which was really much needed
considering how many images and how many gigabytes of data we had to transfer.

Then, we created the bootstrapping architecture that is similar to what the sovereign scenario has.

The generated RGD from all of these was quite enormous. It's +1300 lines of replacements, localizations and installations.

However, using ocm-k8s-toolkit and only four Kubernetes objects:
```yaml
apiVersion: delivery.ocm.software/v1alpha1
kind: Repository
metadata:
  name: thalamus-repository
  namespace: thalamus
spec:
  repositorySpec:
    baseUrl: <target-repo>/thalamus-transferred
    type: OCIRegistry
  interval: 10m
---
apiVersion: delivery.ocm.software/v1alpha1
kind: Component
metadata:
  name: thalamus-component
  namespace: thalamus
spec:
  component: github.com/cobaltcore-dev/thalamus
  repositoryRef:
    name: thalamus-repository
  semver: "0.1.0"
  interval: 1m
---
apiVersion: delivery.ocm.software/v1alpha1
kind: Resource
metadata:
  name: thalamus-resource-rgd
  namespace: thalamus
spec:
  componentRef:
    name: thalamus-component
  resource:
    byReference:
      resource:
        name: thalamus-rgd
---
apiVersion: delivery.ocm.software/v1alpha1
kind: Deployer
metadata:
  name: thalamus-deployer
  namespace: thalamus
spec:
  resourceRef:
    name: thalamus-resource-rgd
    namespace: thalamus
```

... we were able to deploy the entire application architecture without problems with all the image references
pointing to the new locations (target-repository).

### LLM usage and Skill

Because of prior work (explained in the below two points) we did in open-component-model v2, it was quite easy to use a
popular LLM (Claude, for example) to almost one-shot a complete component version for the entire Thalamus project.

#### Schema

The component constructor YAML file follows the schema depicted [here](https://ocm.software/docs/reference/component-constructor/).
This can be provided to the LLM as a basis for generating a valid component version. It doesn't really require any
particular skills on the LLM's side. Just link the schema it needs to read and understand.

Link in the file and tell your favorite agent the following:
> Analyze this JSON schema save its structure in your memory. Any further files names component-constructor.yaml MUST
> use this schema. Anytime I tell you to "create a component constructor" use this schema to generate the corresponding
> YAML file.

Claude Code already has a built-in JSON schema skill.

#### Samples

We created ample of examples that create and consume complex component constructors and RGDs. One of the most complicated
one is the sovereign scenario we put together located here: [Sovereign Conformance Test](https://github.com/open-component-model/open-component-model/tree/main/conformance/scenarios/sovereign).

Before giving the LLM the samples however, you can also fine-tune it's understanding about Kro. Link the RGD schema definition
from here: [Kro RGD JSON Schema](https://raw.githubusercontent.com/kubernetes-sigs/kro/refs/heads/main/helm/crds/kro.run_resourcegraphdefinitions.yaml). It's better to link the YAML file rather
than point the LLM to a website like the rendered schema on Kro's page here: [Rendered RGD](https://kro.run/api/crds/resourcegraphdefinition/). This is
much harder to parse and takes up too much context to process and then keep "in mind". The more specific, the better.

Once the LLM understands Kro, now is a good time to point it at the folder containing the sovereign scenario, then make the
following prompt:

> Read the example in this folder. It contains a complex example for structuring and creating component versions. The example
> is a multi-component product localized via Kro and deployed by Flux. Pay special attention to the localization where
> Kro CEL expressions replace values inside Helm values.yaml files.

Once the model understands it, we can move on to the next step.

#### The Next Step

With this done, you can now begin ocmifying your application. Give the model the following prompt:

> Analyze the deployment and packaging process of this application. (You can be more specific here if you have a helm
> chart just point the LLM at it.). Now, take this information and create a component constructor file that packages all of
> the application components as component references.

Notice that we aren't doing everything in one step. That will just overload the context. So once the component constructor
exists and looks good, you can move on to the deployment part.

> Create a bootstrapping structure for the application component version. The RGD should localize all image references
> found in the deployment helm charts. (If you have kustomize or something else, replace this with that.)

This should already create you a pretty close-by component version and bootstrapping architecture of your application.

## Another choice

Instead of one RGD that rules them all, we also tried creating several RGDs for separate components and then use a root
RGD and [RGD Chaining](https://kro.run/docs/building-abstractions/rgd-chaining/) in Kro. We also constructed separate
component constructors for each individual component. The idea was that separate components could be exchanged for
organization specific ones. For example, if you had your own gpu-operator implementation that used a specific, platform
dependent implementation then you should be able to configure the deployment to use that instead.

We created a plug-and-play style architecture for the components where a component can be exchanged for a different
one and the whole would still be valid and would work as intended.

This was not feasible in the end however, because it introduced a silent API that was not easy to discover and understand by
those who use the component. How would you know what the parts are that you can use _instead_ of the default configured
one? How would you be able to discover new components provided by different component authors? How would you even know
you can mix and match?

And further, if you do change a component and an inevitable failures occurs, a support team wouldn't be able to help you because
you are using a non-standard exchange for a component.

And lastly, deployment ordering and bootstrapping would be difficult to set up. RGD chaining gives you _some_ control
over ordering but having everything in one place makes `dependson` trivial to set up. The real pain would be bootstrapping
_every_ separate component. The bootstrap suddenly was 11 x 3 (eleven times the component, resource, deployer CRs) to deploy
and have all the RGDs in place for reconciliation.

Therefore, we opted for the single component version that describes the root, thus making everything more explicit and
visible.

## What's next

While the [Pull Requests](https://github.com/cobaltcore-dev/thalamus/pull/77) exists it still has some ways to go. Some amount
of cleanup is required and the architecture needs to be finalized and approved.

All-in-all the construction and the deployment was a great success.
