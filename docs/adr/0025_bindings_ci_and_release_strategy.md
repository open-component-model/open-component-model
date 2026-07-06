# ADR Template

* **Status**: draft
* **Deciders**: OCM Technical Steering Committee
* **Date**: 2026-07-06

## Context and Problem Statement
This is the approximate dependency structure of the go modules stored inside the monorepo at time of writing:
```mermaid
flowchart TD
    A_cli["cli"]:::top
    A_k8s["kubernetes/controller"]:::k8s
    B_transfer["transfer"]
    C_helm["helm"]
    D_input_dir["input/dir"]
    D_input_file["input/file"]
    D_input_utf8["input/utf8"]
    D_plugin["plugin"]
    E_constructor["constructor"]
    E_sigstore["sigstore"]
    F_oci["oci"]
    F_signing["signing"]
    G_desc_norm["descriptor/normalisation"]
    G_gpg["gpg"]
    G_repository["repository"]
    G_rsa["rsa"]
    G_transform["transform"]
    H_credentials["credentials"]
    H_ctf["ctf"]
    H_desc_rt["descriptor/runtime"]
    H_http["http"]
    I_blob["blob"]
    I_configuration["configuration"]
    I_desc_v2["descriptor/v2"]
    I_generator["generator"]
    J_cel["cel"]
    J_dag["dag"]
    J_runtime["runtime"]

    I_blob --> J_runtime
    I_configuration --> J_runtime
    E_constructor --> G_desc_norm
    E_constructor --> F_oci
    H_credentials --> I_configuration
    H_credentials --> J_dag
    H_ctf --> I_blob
    G_desc_norm --> H_desc_rt
    H_desc_rt --> I_desc_v2
    I_desc_v2 --> J_runtime
    I_generator --> J_runtime
    G_gpg --> H_credentials
    G_gpg --> H_desc_rt
    C_helm --> D_plugin
    H_http --> I_configuration
    D_input_dir --> E_constructor
    D_input_file --> E_constructor
    D_input_utf8 --> E_constructor
    F_oci --> H_ctf
    F_oci --> H_http
    F_oci --> G_repository
    D_plugin --> E_constructor
    D_plugin --> F_signing
    G_repository --> I_blob
    G_repository --> H_credentials
    G_repository --> H_desc_rt
    G_rsa --> H_credentials
    G_rsa --> H_desc_rt
    F_signing --> G_desc_norm
    E_sigstore --> H_credentials
    E_sigstore --> F_signing
    B_transfer --> C_helm
    B_transfer --> G_transform
    G_transform --> J_cel
    G_transform --> H_credentials
    A_cli --> G_gpg
    A_cli --> D_input_dir
    A_cli --> D_input_file
    A_cli --> D_input_utf8
    A_cli --> G_rsa
    A_cli --> E_sigstore
    A_cli --> B_transfer
    A_k8s --> G_rsa
    A_k8s --> B_transfer

    classDef top fill:#ffb3b3,stroke:#cc6666
    classDef k8s fill:#b3ffb3,stroke:#66cc66
```


Each module has its own semver and to propagate e.g. a change in `runtime`, `dag`, or `cel` we have to release (release module(s), bump `go.mod` & `go.sum` files in next layer, loop) in at least 10 layers:

#### Release layers (bottom-up)

| Layer | Modules | Notes |
|-------|---------|-------|
| 0 | runtime, dag, cel | Foundation — no internal deps |
| 1 | descriptor/v2, configuration, blob, generator | Only depend on runtime |
| 2 | descriptor/runtime, credentials, http, ctf | Core infra |
| 3 | descriptor/normalisation, repository, transform, gpg, rsa | Mid-level |
| 4 | signing, oci | Storage + signing |
| 5 | constructor, sigstore | Build + verify |
| 6 | plugin, input/dir, input/file, input/utf8 | Extensions |
| 7 | helm | Chart support |
| 8 | transfer | High-level orchestration |
| 9 | cli, kubernetes/controller | Top-level consumers |

This complexity is currently managed by the developers and can get particularly challening when logic has to be adjust across multiple modules at once.
This ADR is concerned with the possible ways the developer experience could be optimized with regards to development of the bindings.


## Decision Drivers

* <Driver 1>
* <Driver 2>
* <Driver 3>

## Considered Options

* Option 1 <Brief description>
* Option 2 <Brief description>
* Option 3 <Brief description>

## Decision Outcome

Chosen [Option X](#option-x): "<Chosen Option>".

Justification:

* <Justification point 1>
* <Justification point 2>
* <Justification point 3>

### Option X

#### Description

<Explain why this option was chosen and its benefits.>

#### High-level Architecture

<Provide a diagram or sequence flow if applicable.>

#### Contract

<Define the interfaces, protocols, and agreements needed for this decision.>

## Pros and Cons of the Options

### [Option 1] <Option Name>

Pros:

* <Pro 1>
* <Pro 2>

Cons:

* <Con 1>
* <Con 2>

### [Option 2] <Option Name>

Pros:

* <Pro 1>
* <Pro 2>

Cons:

* <Con 1>
* <Con 2>

### [Option 3] <Option Name>

Pros:

* <Pro 1>
* <Pro 2>

Cons:

* <Con 1>
* <Con 2>

## Discovery and Distribution

<Explain how the decision will be implemented, distributed, and maintained.>

## Conclusion

<Summarize the decision and its expected impact.>
