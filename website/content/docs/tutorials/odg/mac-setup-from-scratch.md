---
title: "Setup from Scratch (macOS)"
description: "This is a detailed, opinionated walkthrough of setting up an Open Delivery Gear (ODG) cluster locally on an Apple Silicon Mac with the Colima container…"
weight: 20
toc: true
---
This is a detailed, opinionated walkthrough of setting up an Open Delivery Gear
(ODG) cluster locally on an **Apple Silicon Mac** with the [Colima](https://colima.run/) container runtime, from zero to a running cluster.
For the concise reference, see
[Deploying the Open Delivery Gear Locally]({{< relref "/docs/how-to/odg/local-setup.md" >}}).

## 1. Install the tooling

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

<details>
<summary>Verify Colima works</summary>
<div markdown="1">

`docker ps` should now work. Run a test container:

```bash
docker run hello-world
# "This message shows that your installation appears to be working correctly."
```

</div>
</details>

<details>
<summary>Recommended: Install the GitHub CLI</summary>
<div markdown="1">

The OCM installer uses `gh` to verify the install.

```bash
brew install gh
gh auth login   # GitHub.com → ... → Login with a web browser
```

</div>
</details>

## 2. Install the OCM CLI

Follow the steps outlined here:
[OCM install guide](https://ocm.software/docs/getting-started/install-the-ocm-cli/).

{{< callout "note" >}}
To verify the binary manually (requires `gh`):

```bash
gh attestation verify "$HOME/.local/bin/ocm" \
  --repo open-component-model/open-component-model
# ✓ Verification succeeded!
```

{{< /callout >}}

## 3. Clone the repository

```bash
git clone git@github.com:open-component-model/open-delivery-gear.git
cd open-delivery-gear
```

## 4. Configure secrets and values

ODG needs two things to start: A **GitHub App** (server-to-server access) and
**OIDC login** for the dashboard. Both go into `secrets-local.yaml`. We skip
private registry config here — the main ODG images are public.

{{< callout "note" >}}
The main cluster config lives in `local-setup/kind/values.yaml`. You
don't need to edit it, but the
[values documentation](https://github.com/open-component-model/odg-core/blob/master/charts/bootstrapping/values.documentation.yaml)
describes every field.
{{< /callout >}}

### Create the GitHub App

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
   | Request user authorization (OAuth) during installation | ✅ Enabled |
   | Webhook | ❌ Disabled |

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

### Fill in `secrets-local.yaml`

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

```
requests.exceptions.HTTPError: 401 Client Error: Unauthorized for url:
http://delivery-service.odg.svc.cluster.local:8080/auth?...&api_url=https://api.github.com
```

### Create `values-local.yaml`

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

<details>
<summary>Why this is needed</summary>
<div markdown="1">

Without it, `delivery-db-0` crash-loops because it can't create its data
directory:

```
delivery-db-0   0/1   CrashLoopBackOff
mkdir: can't create directory '/data/pgdata': Permission denied
```

No special permissions are needed on the local folder.

</div>
</details>

## 5. Start the cluster

```bash
make kind-up
```

{{< callout "info" >}}
On errors, run `make kind-down` before retrying. Use it to shut down the
cluster too.
{{< /callout >}}

<details>
<summary>Fixing "failed to render components"</summary>
<div markdown="1">

```
Error: failed to render components: ... no roots found in the dag
```

Set an explicit version. Find the current one on the
[releases page](https://github.com/open-component-model/open-delivery-gear/releases):

```bash
ODG_VERSION=0.26.0 make kind-up
```

</div>
</details>

On success, log in to the dashboard at `http://localhost:3000` with GitHub.

## 6. Access the cluster with kubectl

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
