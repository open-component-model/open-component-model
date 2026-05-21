---
title: "model-server: Serving ML Models from OCM with HF Hub, Ollama, OpenAI, and MLflow APIs"
date: 2026-05-21T10:00:00+02:00
draft: false
description: "model-server makes OCM repositories look like Hugging Face Hub, Ollama, OpenAI, and MLflow Model Registry — so existing ML tooling works against signed, versioned, supply-chain-auditable components without modification."
summary: "A look at model-server, a new Go server that bridges OCM's supply-chain guarantees with the four API surfaces ML practitioners already use every day."
categories: ["ecosystem"]
tags: ["models", "machine-learning", "supply-chain"]
contributors: []
---

Machine learning has a distribution problem. Model weights are large, often unsigned, and arrive via ad-hoc scripts that `wget` from S3 buckets, Hugging Face Hub, or wherever the author happened to upload them. There is no provenance, no signing, no version graph, and no audit trail. For teams that take supply-chain security seriously, this is a gap.

[OCM](https://ocm.software) already solves this for software artifacts: components are versioned, signed, and stored in standard OCI registries. The question is whether the same infrastructure can serve ML models — and whether it can do so without forcing ML practitioners to abandon the tools they already know.

[model-server](https://github.com/jakobmoellerdev/model-server) is the answer to that question.

---

## What model-server does

model-server is a Go HTTP server that sits in front of one or more OCM repositories (OCI registries or CTF archives) and presents their contents as four familiar API surfaces simultaneously:

- **Hugging Face Hub** — `/api/models`, `/api/models/{owner}/{model}`, `/{owner}/{model}/resolve/{rev}/{file}`
- **Ollama** — `/api/tags`, `/api/show`, `/api/pull`
- **OpenAI** — `/v1/models`, `/v1/models/{model}`
- **MLflow Model Registry** — `/api/2.0/mlflow/registered-models/*`, `/api/2.0/mlflow/model-versions/*`

Clients — the HF Python SDK, the `ollama` CLI, the OpenAI SDK, or `mlflow.MlflowClient` — connect to model-server with a single environment variable change and work without modification. The models they are pulling are OCM components: signed, versioned, and with full provenance.

```text
┌──────────────────────────────────────────────────────────┐
│               model-server                               │
│                                                          │
│  HF Hub API  │  Ollama API  │  OpenAI API  │  MLflow API │
│──────────────────────────────────────────────────────────│
│              ModelRegistry (in-memory index)             │
│──────────────────────────────────────────────────────────│
│   OCM Client  →  OCI Registry / CTF Archive              │
└──────────────────────────────────────────────────────────┘
```

There is no inference, no proxying, and no data transformation. model-server is a pure distribution layer.

---

## Models as OCM components

Each model is an OCM component version. The server discovers models by reading labels in the `ext.ocm.software/model-server.*` namespace on the component descriptor.

A minimal component looks like this:

```yaml
components:
  - name: github.com/my-org/models/llama-3-8b
    version: 1.0.0
    provider:
      name: my-org
    labels:
      - name: ext.ocm.software/model-server.model-id
        value: "meta-llama/Llama-3-8B"
      - name: ext.ocm.software/model-server.task
        value: "text-generation"
      - name: ext.ocm.software/model-server.license
        value: "llama3"
    resources:
      - name: config
        type: modelConfig
        relation: local
        input:
          type: file
          path: weights/config.json
          mediaType: application/json
        labels:
          - name: ext.ocm.software/model-server.filename
            value: "config.json"
      - name: weights
        type: modelWeights
        relation: local
        input:
          type: file
          path: weights/model.safetensors
          mediaType: application/octet-stream
        labels:
          - name: ext.ocm.software/model-server.filename
            value: "model.safetensors"
          - name: ext.ocm.software/model-server.lfs
            value: "true"
```

The `model-id` label is the public identifier clients use — the same string you would pass to `hf_hub_download` or `ollama pull`. The OCM component name is the internal storage key. This separation means you can use any OCM component naming convention internally while presenting a clean, familiar model ID to consumers.

### Resource types

| OCM type | Purpose |
| --- | --- |
| `modelWeights` | Model weights (`*.safetensors`, `*.gguf`, `*.bin`) |
| `modelConfig` | `config.json`, `tokenizer_config.json`, etc. |
| `modelCard` | `README.md` |
| `tokenizer` | Tokenizer files |

### Optional labels

| Label | Description | Example |
| --- | --- | --- |
| `ext.ocm.software/model-server.library` | Framework | `transformers` |
| `ext.ocm.software/model-server.family` | Model family | `llama` |
| `ext.ocm.software/model-server.gated` | Gated model flag | `true` |
| `ext.ocm.software/model-server.private` | Private model flag | `true` |
| `ext.ocm.software/model-server.lfs` | Serve as LFS pointer | `true` |

---

## Quick start

### 1. Build

```bash
git clone https://github.com/jakobmoellerdev/model-server
cd model-server
go build -o bin/model-server ./cmd/model-server
```

### 2. Configure

Create `model-server.yaml`. To use the published sample component:

```yaml
server:
  listen: ":8080"

auth:
  mode: none

ocm:
  repositories:
    - name: sample
      type: OCIRegistry
      url: ghcr.io/jakobmoellerdev/model-server/models

  signatures:
    required: false

apis:
  hfhub:
    enabled: true
  ollama:
    enabled: true
  openai:
    enabled: true
  mlflow:
    enabled: true
```

For a local CTF archive (no registry required):

```yaml
ocm:
  repositories:
    - name: local
      type: CTF
      url: /path/to/models.ctf
```

### 3. Run

```bash
bin/model-server -config model-server.yaml
```

The server builds its index on startup by reading all components in the configured repositories. It is ready when `GET /readyz` returns `{"status":"ready"}`.

---

## Using the four APIs

All examples below use the published sample component `example-org/tiny-model`.

### Hugging Face Hub

```python
import os
os.environ["HF_ENDPOINT"] = "http://localhost:8080"

from huggingface_hub import HfApi, hf_hub_download

api = HfApi(endpoint="http://localhost:8080", token="any")

# List models
for model in api.list_models():
    print(model.id, model.pipeline_tag)

# Model metadata
info = api.model_info("example-org/tiny-model")
print(info.card_data.license)

# Download a file
path = hf_hub_download(
    repo_id="example-org/tiny-model",
    filename="config.json",
    endpoint="http://localhost:8080",
    token="any",
)
```

With `transformers`:

```python
os.environ["HF_ENDPOINT"] = "http://localhost:8080"
from transformers import AutoConfig
config = AutoConfig.from_pretrained("example-org/tiny-model", token="any")
```

### Ollama

```bash
export OLLAMA_HOST=http://localhost:8080

ollama list
# NAME                            ID              SIZE
# example-org/tiny-model:1.0.0    ...             2.4 KB

ollama pull example-org/tiny-model
ollama show example-org/tiny-model
```

### OpenAI SDK

```python
from openai import OpenAI

client = OpenAI(base_url="http://localhost:8080/v1", api_key="any")

for model in client.models.list():
    print(model.id, model.owned_by)

model = client.models.retrieve("example-org/tiny-model")
print(model.id, model.created)
```

### MLflow

```python
import mlflow
mlflow.set_tracking_uri("http://localhost:8080")
client = mlflow.MlflowClient()

for rm in client.search_registered_models():
    print(rm.name, rm.latest_versions[0].version)

uri = client.get_model_version_download_uri("example-org/tiny-model", "1.0.0")
print(uri)  # http://localhost:8080/example-org/tiny-model/resolve/1.0.0/
```

---

## The sample component

A synthetic toy model (`example-org/tiny-model`) is published at `ghcr.io/jakobmoellerdev/model-server/models` for testing. It contains:

- `config.json` — a minimal transformer-style config
- `tokenizer.json` — a stub BPE tokenizer with four special tokens
- `README.md` — a model card
- `weights.bin` — a 16-byte stub for weight download testing

The component is built with the OCM CLI and published via GitHub Actions on every release. The source is in [`examples/component/`](https://github.com/jakobmoellerdev/model-server/tree/main/examples/component).

---

## Supply-chain guarantees

model-server inherits OCM's full supply-chain stack. Because models are OCM components:

**Signing**: Set `signatures.required: true` in the config and model-server will reject any component version that has not been signed with a trusted key. This is enforced at index-build time, before any client request reaches the server.

**Provenance**: Every component version has a deterministic digest. The `X-Repo-Commit` header returned on file downloads is the component version string, giving clients a stable reference to the exact artifact they received.

**Multi-registry federation**: Configure multiple OCM repositories and model-server merges them into a single index. You can serve models from GHCR, AWS ECR, and an on-premises OCI registry simultaneously under one endpoint.

**Offline / air-gapped deployment**: Point model-server at a CTF archive and it serves models with zero network dependency. The same archive can be produced by `ocm transfer` from a connected environment.

```yaml
ocm:
  repositories:
    - name: internal-ecr
      type: OCIRegistry
      url: 123456789.dkr.ecr.us-east-1.amazonaws.com/models
      credentialsRef: ecr-creds
    - name: air-gapped-backup
      type: CTF
      url: /mnt/nfs/models-snapshot.ctf
```

---

## Observability

The server exposes standard observability endpoints:

- `GET /healthz` — liveness probe
- `GET /readyz` — readiness probe (returns ready once the index is built)
- `GET /metrics` — Prometheus metrics including request counts, latency histograms, and index size

---

## Authentication

The `auth.mode` field supports three values:

- `none` — no authentication (suitable for internal / private networks)
- `bearer` — static bearer tokens loaded from a file
- `oidc` — OIDC token validation

For a trusted internal deployment serving pre-approved models, `none` is the right default. For externally exposed endpoints or multi-tenant environments, `bearer` or `oidc` provide per-token access control without adding infrastructure dependencies.

---

## What's next

model-server is at v0.2.0. The near-term roadmap includes:

- **Signature verification UI** — surface signing metadata through the HF Hub and MLflow APIs so consumers can inspect provenance without leaving their tools
- **Credential passthrough** — allow per-request OCI credentials for multi-tenant deployments
- **Range request support** — efficient partial downloads for large weight files without buffering the full blob

Contributions and feedback are welcome at [github.com/jakobmoellerdev/model-server](https://github.com/jakobmoellerdev/model-server).

{{< callout type="tip" >}}
Run the full set of example scripts against the sample component with:

```bash
git clone https://github.com/jakobmoellerdev/model-server
cd model-server
bin/model-server -config examples/config/model-server.yaml &
bash examples/usage/curl.sh
```
{{< /callout >}}
