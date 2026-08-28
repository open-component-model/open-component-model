---
title: "Setting up a hybrid dev setup"
description: "Run ODG code locally connected to a running ODG instance."
weight: 1
toc: true
---

## Goal

Run ODG code (e.g. to test a new extension) locally and connect it to a running ODG instance.

## You'll end up with

- A local environment capable of running ODG packages and libraries connected to a live ODG instance
- Local code focused on the payload, with Kubernetes and API connectivity handled via config

**Estimated time:** ~15 minutes

## Prerequisites

- An ODG instance and a kubeconfig (using static credentials)
- `python3`, `uv`
- GitHub Token authorised to access ODG API

## Steps

### Pull the ODG Core repository and install dependencies

Pull the [ODG Core repository](https://github.com/open-component-model/odg-core) and install dependencies using `uv`.

```shell
uv sync
```

{{< callout type="tip" >}}
If you are not using virtual environments, you have to additionally provide the `--break-system-packages` flag.
{{< /callout >}}

### Configure ODG

Configuration and Secrets are expected to be available via local file paths.
In a Kubernetes environment, this is implemented using mounted ConfigMaps and Secrets.
On a local machine these files are created manually.

The [ODG Core repository](https://github.com/open-component-model/odg-core) features blueprints and default configuration files already.
They can just be adjusted, as ODG will use them by default.

- [Configuring your Extensions](https://github.com/open-component-model/odg-core/blob/master/odg/extensions_cfg.yaml)
- [Configuring your supported Findings](https://github.com/open-component-model/odg-core/blob/master/odg/findings_cfg.yaml)

An in-depth documentation for all the available configuration options is [here](https://github.com/open-component-model/odg-core/blob/master/charts/bootstrapping/values.documentation.yaml).

### Configure secrets

ODG features a typed Secret system. There is an opinion on how a secret has to be structured and named.
The [secrets directory](https://github.com/open-component-model/odg-core/tree/master/secrets) features templates for supported secret types. To make them effective, the `.template` string has to be dropped from the filename.

Also for the secret structure and semantics, [there is detailed documentation](https://github.com/open-component-model/odg-core/blob/c8b4f4fe055b8719a0dd849026678f6ab8127d76/charts/bootstrapping/values.documentation.yaml#L1558).

### Connect to the ODG instance via Kubernetes

To connect ODG code to a running cluster, obtain a valid `kubeconfig` (with static credentials) and put it into a local file.
ODG will pick it up when referenced via `--kubeconfig` parameter on startup.

```shell
python3 -m my-odg-extension --kubeconfig /path/to/kubeconfig
```

### Connect to the ODG API

Depending on the GitHub instances the ODG API supports, create a GitHub secret in the ODG secret structure.

```bash
cat << 'EOF' > /path/to/odg-api/secrets/github/odg-api.yaml
api_url: https://api.github.com
http_url: https://github.com
username: <your-username>
auth_token: <your-github-token>
repo_urls: []
EOF
```

On ODG code startup, provide the URL to the ODG API with `--delivery-service-url` parameter.

```shell
python3 -m my-odg-extension --delivery-service-url 'https://delivery-service.demo.ci.gardener.cloud'
```

### Ensure code quality

The [ODG Core repository](https://github.com/open-component-model/odg-core) features linters, formatters, and tests. There are callbacks to run them properly configured.

```bash
.ci/lint
.ci/check-format
.ci/test
```

## Troubleshooting

### Symptom: `uv sync` fails or packages cannot be installed

**Cause:** System Python environment is managed and rejects package installs without explicit consent.

**Fix:** Add `--break-system-packages` flag to the install command.

## Next steps

- [Deploy ODG locally]({{< relref "docs/how-to/odg/deploying-the-open-delivery-gear-locally/" >}})

## Related documentation

- [ODG System Architecture]({{< relref "docs/concepts/odg/odg-system-architecture/" >}})
