---
title: "Setup from Scratch (macOS)"
description: "Detailed walkthrough of setting up an ODG cluster locally on Apple Silicon Mac."
weight: 2
toc: true
---

This tutorial is a detailed, opinionated walkthrough of setting up an [Open Delivery Gear]({{< relref "docs/concepts/odg/odg-system-architecture/" >}})
(ODG) cluster locally on an **Apple Silicon Mac** with the [Colima](https://colima.run/) container runtime, from zero to a running cluster.

## What You'll Learn

- How to install and configure all required tooling on macOS (Colima, kind, OCM CLI, Helm)
- How to configure GitHub App credentials and OIDC login for the ODG dashboard
- How to start a local ODG cluster and access it with `kubectl`

## How It Works

The setup uses [kind](https://kind.sigs.k8s.io/) to run a Kubernetes cluster inside Docker (managed by Colima), bootstrapped via a `make kind-up` target that applies all ODG Helm charts. A `secrets-local.yaml` file supplies GitHub App and OAuth credentials at cluster creation time.

**Estimated time:** ~20 minutes

## Prerequisites

- Apple Silicon Mac (M1/M2/M3)
- [Homebrew](https://brew.sh/) installed
- A GitHub account with permission to create GitHub Apps
- For the concise reference, see [Deploying the Open Delivery Gear Locally]({{< relref "docs/how-to/odg/deploying-the-open-delivery-gear-locally/" >}})

## Scenario

Throughout this tutorial:

- The local dashboard will be available at `http://localhost:3000`
- The Postgres volume will be mounted at `~/odg-postgres-data`
- The kind cluster will be named `kind-odg-local`

## Tutorial Steps

{{< steps >}}

{{< step >}}

### Install the tooling

```bash
brew install kubectl k9s colima docker docker-compose kind helm wget yq
```

- `kubectl` / `k9s` — interact with the cluster
- `colima` — container runtime (Docker Desktop works too)
- `kind` — runs the local cluster
- `helm`, `wget`, `yq` — required by the setup scripts

Start Colima:

```bash
colima start --mount-type=virtiofs
```

{{< callout type="note" >}}
`docker ps` should now work. Run a test container:
```bash
docker run hello-world
# "This message shows that your installation appears to be working correctly."
```
{{< /callout >}}

{{< callout type="note" >}}
The OCM installer uses `gh` to verify the install.
```bash
brew install gh
gh auth login   # GitHub.com → ... → Login with a web browser
```
{{< /callout >}}
{{< /step >}}

{{< step >}}

### Install the OCM CLI

Follow the steps outlined here:
[OCM install guide](https://ocm.software/docs/getting-started/install-the-ocm-cli/).

{{< callout type="note" >}}
To verify the binary manually (requires `gh`):
```bash
gh attestation verify "$HOME/.local/bin/ocm" \
  --repo open-component-model/open-component-model
# Verification succeeded!
```
{{< /callout >}}
{{< /step >}}

{{< step >}}

### Clone the repository

```bash
git clone git@github.com:open-component-model/open-delivery-gear.git
cd open-delivery-gear
```
{{< /step >}}

{{< step >}}

### Configure secrets and values

ODG needs two things to start: A **GitHub App** (server-to-server access) and
**OIDC login** for the dashboard. Both go into `secrets-local.yaml`. We skip
private registry config here — the main ODG images are public.

{{< callout type="note" >}}
The main cluster config lives in `local-setup/kind/values.yaml`. You
don't need to edit it, but the
[values documentation](https://github.com/open-component-model/odg-core/blob/master/charts/bootstrapping/values.documentation.yaml)
describes every field.
{{< /callout >}}

#### Create the GitHub App

ODG uses a GitHub App for server-to-server access (reading repos, creating
issues/PRs, checking security alerts).

1. GitHub → your account **Settings** → **Developer settings** → **GitHub Apps**
   → **New GitHub App**.
2. Fill in the form:

   | Field | Value |
   | --- | --- |
   | GitHub App name | Something unique, e.g. `yourname-odg-local` |
   | Homepage URL | `http://localhost:3000` |
   | Callback URL | `http://localhost:3000` |
   | Request user authorization (OAuth) during installation | Enabled |
   | Webhook | Disabled |

3. Under **Permissions & events**, set:
   - **Repository**: Contents, Issues, Pull requests → Read & Write
   - (optional) **Repository**: Code / Secret scanning alerts → Read (for for the CodeQL and GHAS extensions)
   - **Organization**: Members → Read-only

   These extra permissions let plugins access GitHub (e.g. post findings). Org
   permissions enable OIDC team/org membership checks.

4. **Install App** → install on your account or org, choosing which repos ODG
   may access. You'll be redirected to a URL containing your
   `installation_id`, e.g.
   `http://localhost:3000/?code=...&installation_id=151722591&setup_action=install`.

#### Fill in `secrets-local.yaml`

Copy the example and edit it — the setup script picks it up automatically:

```bash
cp local-setup/secrets-local.yaml.example local-setup/secrets-local.yaml
```

**`github-app`** section:

```yaml
secrets:
  github-app:
    github_com:
      api_url: https://api.github.com
      app_id: ... # your app id e.g 1234567
      mappings:
        - installation_id: ... # your installation id e.g. 12345678
          org: ... # your-username (unless you created an org-wide app)
      private_key: |
        -----BEGIN RSA PRIVATE KEY-----
        ...
        -----END RSA PRIVATE KEY-----
```

| Field | Where to find it |
| --- | --- |
| `app_id` | App settings → "App ID" (`https://github.com/settings/apps/`) |
| `installation_id` | Installation URL: `https://github.com/settings/installations/...` |
| `org` | Your username if installed on your account |
| `private_key` | App settings → bottom → "Generate a private key", paste file contents |

**`oauth-cfg`** section — enables dashboard login. `role_bindings` maps GitHub
identities to ODG roles:

```yaml
secrets:
  ...
  oauth-cfg:
    local:
      client_id: Iv...
      client_secret: ...
      api_url: https://api.github.com
      type: github
      name: GitHub
      role_bindings:
        - roles: [admin]
          subjects:
            - type: github-user
              name: your-username
            - type: github-app
              name: your-app-name
```

| Field | Where to find it |
| --- | --- |
| `client_id` | GitHub App settings → "Client ID" |
| `client_secret` | GitHub App settings → generate a client secret |

Possible **role types** for `role_bindings` are `admin`, `reader` and `writer`.

Possible **subject types** for `role_bindings`:

| Subject type | Meaning | Example |
| --- | --- | --- |
| `github-user` | GitHub username (regex) | `alice` |
| `github-org` | Members of an org | `my-org` |
| `github-team` | Members of a team, `org/team-slug` | `my-org/platform-team` |
| `github-app` | GitHub App slug (regex) | `yourname-odg-local` |

The `github-app` permission allows extensions such as the artefact enumerator and cache manager to
authenticate against GitHub. Otherwise you might find such errors:

```text
requests.exceptions.HTTPError: 401 Client Error: Unauthorized for url:
http://delivery-service.odg.svc.cluster.local:8080/auth?...&api_url=https://api.github.com
```

#### Create `values-local.yaml`

Colima auto-mounts `$HOME` but not paths like `/var/`, so the Postgres volume
fails on its default `/var/delivery-db` mount. Point it at a subfolder in your home directory instead:

```bash
cp local-setup/values-local.yaml.example local-setup/values-local.yaml
```

```yaml
persistence:
  hostPath: "/Users/<your-username>/odg-postgres-data"
  containerPath: "/var/delivery-db"
```

{{< callout type="note" >}}
Without it, `delivery-db-0` crash-loops because it can't create its data
directory:
```text
delivery-db-0   0/1   CrashLoopBackOff
mkdir: can't create directory '/data/pgdata': Permission denied
```
No special permissions are needed on the local folder.
{{< /callout >}}
{{< /step >}}

{{< step >}}

### Start the cluster

```bash
make kind-up
```

{{< callout type="note" >}}
On errors, run `make kind-down` before retrying. Use it to shut down the
cluster too.
{{< /callout >}}

{{< callout type="note" >}}
If you see `Error: failed to render components: ... no roots found in the dag`,
set an explicit version. Find the current one on the
[releases page](https://github.com/open-component-model/open-delivery-gear/releases):
```bash
ODG_VERSION=0.26.0 make kind-up
```
{{< /callout >}}

On success, log in to the dashboard at `http://localhost:3000` with GitHub.
{{< /step >}}

{{< step >}}

### Access the cluster with kubectl

The setup writes a kubeconfig to `local-setup/kind/kubeconfig`. Add this to your
`~/.zshrc` so the context is always available:

```bash
export KUBECONFIG="$HOME/<your-code-dir>/open-delivery-gear/local-setup/kind/kubeconfig:$HOME/.kube/config"
```

After sourcing, select the context and check the pods:

```bash
kubectl config use-context kind-odg-local
kubectl get pods
# NAME                                  READY   STATUS      RESTARTS   AGE
# backlog-controller-...                1/1     Running     0          33m
# delivery-dashboard-...                1/1     Running     0          33m
# delivery-db-0                         1/1     Running     0          34m
# delivery-service-...                  1/1     Running     0          34m
```
{{< /step >}}

{{< /steps >}}

## What you've learned

- How to install Colima, kind, the OCM CLI, and all supporting tools on macOS
- How to create a GitHub App and populate `secrets-local.yaml` with its credentials
- How to configure Postgres persistence for Colima's `virtiofs` mount constraints
- How to start and stop the ODG cluster with `make kind-up` / `make kind-down`
- How to set up `KUBECONFIG` and verify pods are running with `kubectl`

## Check your understanding

- [ ] Why must the Postgres `hostPath` point to a directory under `$HOME` on Colima?
- [ ] What does the `github-app` subject type in `role_bindings` control?
- [ ] What command should you run before retrying `make kind-up` after an error?

{{< details "Answers & Explanations">}}
**1. Why must the Postgres `hostPath` point to a directory under `$HOME` on Colima?**
Colima's `virtiofs` mount only auto-mounts `$HOME`. Paths like `/var/` are not accessible inside the VM, so the Postgres pod would fail to create its data directory and enter `CrashLoopBackOff`.

**2. What does the `github-app` subject type in `role_bindings` control?**
It grants ODG extensions (such as the artefact enumerator and cache manager) permission to authenticate against GitHub using the GitHub App. Without it, those extensions receive 401 Unauthorized errors when calling the delivery-service auth endpoint.

**3. What command should you run before retrying `make kind-up` after an error?**
`make kind-down` — this tears down the existing (partially created) cluster so the next `make kind-up` starts from a clean state.
{{< /details >}}

## Troubleshooting

**Symptom:** `delivery-db-0` is in `CrashLoopBackOff` with `Permission denied` on `/data/pgdata`.
**Cause:** The Postgres `hostPath` is set to `/var/delivery-db`, which Colima cannot mount.
**Fix:** Set `persistence.hostPath` in `values-local.yaml` to a path under `$HOME`, e.g. `/Users/<your-username>/odg-postgres-data`.

---

**Symptom:** `make kind-up` fails with `no roots found in the dag`.
**Cause:** The default version resolution failed (network issue or no tagged release found).
**Fix:** Pin an explicit version: `ODG_VERSION=0.26.0 make kind-up`.

---

**Symptom:** Extensions log `401 Client Error: Unauthorized` when calling the delivery-service.
**Cause:** The GitHub App slug is not listed as a subject in `role_bindings` in `secrets-local.yaml`.
**Fix:** Add a `github-app` subject entry with your app's slug under the appropriate role in `oauth-cfg.role_bindings`.

## Next steps

- [Deploying the Open Delivery Gear Locally]({{< relref "docs/how-to/odg/deploying-the-open-delivery-gear-locally/" >}})
- [Setting up a hybrid dev setup]({{< relref "docs/how-to/odg/setting-up-a-hybrid-dev-setup/" >}})
- [Contribute a new Extension]({{< relref "docs/tutorials/odg/contribute-a-new-extension/" >}})

## Related documentation

- [ODG System Architecture]({{< relref "docs/concepts/odg/odg-system-architecture/" >}})
- [Reacting upon OCM events]({{< relref "docs/concepts/odg/reacting-upon-ocm-events/" >}})
- [Lifecycling GitHub issues]({{< relref "docs/concepts/odg/lifecycling-github-issues/" >}})
