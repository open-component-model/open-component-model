---
title: "TEMPLATE: Tutorial Title"
description: "A guided, learning-focused walkthrough that teaches you how to accomplish a goal in OCM."
weight: 999
toc: true
---

{{< callout context="note" title="Enable Mermaid diagrams" >}}
If your tutorial includes Mermaid diagrams, add this to the frontmatter:
```yaml
---
hasMermaid: true
---
```
{{< /callout >}}

Write 1–2 sentences that set the scene and explain *why* you would do this.
Avoid deep background. Link to the relevant concept(s) instead.

## What You'll Learn

By the end of this tutorial, you will:

- What you can do by the end (outcome #1)
- One key concept you will understand (outcome #2)
- How to validate your success (outcome #3)

## How It Works

Use a Mermaid diagram to visualize the workflow or architecture you'll be working with.
This gives learners a mental model before diving into steps.

```mermaid
flowchart LR
    A[Input/Start] -- "command" --> B[OCM Artifact]
    B -- "command" --> C[(Output/Registry)]
```

Write 1–2 sentences explaining the workflow shown in the diagram.

**Estimated time:** ~X minutes

## Prerequisites

- [OCM CLI]({{< relref "docs/getting-started/ocm-cli-installation.md" >}}) installed
- Access to required repositories/services (example: `ghcr.io`)
- Any required credentials (example: GitHub token with package write access)

## Scenario

Describe the concrete scenario you'll use throughout. Use specific values consistently. Readers will copy/paste these.

- **Component:** `github.com/acme.org/helloworld:1.0.0`
- **Repository:** `ghcr.io/acme-ocm`
- **Working directory:** `/tmp/ocm-tutorial`

{{< callout type="tip" >}}
**Handling variations:** If your tutorial covers multiple paths (e.g., different resource types or deployment targets),
use tabs to keep the learning flow clear:
{{< /callout >}}

{{< tabs "example-tabs" >}}
{{< tab "Helm Chart" >}}
```yaml
type: helmChart
input:
  type: helm
  path: ./chart
```
{{< /tab >}}
{{< tab "OCI Image" >}}
```yaml
type: ociImage
input:
  type: dockermulti
  repository: ghcr.io/myorg/myimage
```
{{< /tab >}}
{{< /tabs >}}

## Tutorial Steps

{{< steps >}}
{{< step >}}

### Create the component constructor file

Explain *what* you're doing and *why* in 1–2 sentences.

```bash
touch component-constructor.yaml
```

Create and save this content to `component-constructor.yaml`:

```yaml
# yaml-language-server: $schema=https://ocm.software/latest/schemas/bindings/go/constructor/schema-2020-12.json
components:
- name: github.com/acme.org/helloworld
  version: 1.0.0
  provider:
    name: acme.org
  resources:
    - name: mylocalfile
      type: blob
      input:
        type: file
        path: ./my-resource.txt
```
{{< /step >}}

{{< step >}}

### Build the component version

Run the OCM CLI to create a CTF archive:

```bash
ocm add cv
```

You should see:
Hide complex output behind a details block:

{{< details "Expected output">}}
```text
 COMPONENT                      │ VERSION │ PROVIDER
────────────────────────────────┼─────────┼──────────
 github.com/acme.org/helloworld │ 1.0.0   │ acme.org
```
{{< /details >}}

This indicates your component version was successfully created.

**Adding optional details:** Use the `{{< details >}}` shortcode to provide technical deep-dives without disrupting the learning flow:

{{< details "Optional: Understanding CTF internals" >}}
The CTF archive uses OCI artifact format internally. Each component version
is stored as an OCI manifest with the component descriptor as a layer.

```shell
tree transport-archive
```

This allows CTF archives to be compatible with OCI registries and tools.
{{< /details >}}

{{< /step >}}

{{< step >}}

### Verify the result**

Check that your component was created correctly:

```bash
ocm get cv ./transport-archive//github.com/acme.org/helloworld:1.0.0
```

You should see your component listed with version 1.0.0.

{{< details "Expected output">}}
```text
COMPONENT                      │ VERSION │ PROVIDER
───────────────────────────────┼─────────┼──────────
github.com/acme.org/helloworld │ 1.0.0   │ acme.org
```
{{< /details >}}

{{< /step >}}
{{< /steps >}}

## What you've learned

Summarize key learning points in 3–6 bullets:

- You created a component constructor file that defines metadata and resources
- You used `ocm add cv` to build a transportable CTF archive
- You verified your component structure using `ocm get cv`

**For deeper understanding:**

- [Concept: <name>]({{< relref "docs/concepts/<file>.md" >}})
- [Concept: <name>]({{< relref "docs/concepts/<file>.md" >}})

## Check your understanding

Before moving on:

- [ ] What is the purpose of a component constructor file?
- [ ] Why do we store component versions in CTF archives?
- [ ] How would you add a second resource?

{{< details "Answers & Explanations">}}

- **Question 1:** Brief answer with explanation
- **Question 2:** Brief answer with explanation  
- **Question 3:** Brief answer with explanation

{{< /details >}}

## Troubleshooting

Common issues in *this tutorial*:

### Problem: Command fails with "component constructor not found"

**Cause:** The `ocm` CLI looks for `component-constructor.yaml` in your current directory.

**Fix:**

```bash
ocm add cv --file /path/to/component-constructor.yaml
```

### Problem: "Invalid version format" error

**Cause:** OCM requires semantic versioning (e.g., `1.0.0`).

**Fix:** Update your `version` field to `MAJOR.MINOR.PATCH` format.

## Cleanup

Remove the resources created in this tutorial:

```bash
rm -rf transport-archive
rm -rf /tmp/ocm-tutorial
rm component-constructor.yaml
```

{{< callout type="warning" >}}
⚠️ This will permanently delete your CTF archive and all component versions it contains.
{{< /callout >}}

## Next steps

Add links to related guides where you DO or LEARN more about the topic.

- [How-to: <name>]({{< relref "docs/how-to/<file>.md" >}})
- [Tutorial: <name>]({{< relref "docs/tutorials/<file>.md" >}})

## Related documentation

Add links to related concepts and references that explain the WHY and provide more details.

- [Concept: <name>]({{< relref "docs/concepts/<file>.md" >}})
- [Reference: <command>]({{< relref "docs/reference/<file>.md" >}})

---

## ✓ Before publishing

Make sure to comply with our [CONTRIBUTING guide](../CONTRIBUTING.md)
and ensure the following:

- [ ] Title describes what learner will accomplish
- [ ] Consistent "you" voice throughout
- [ ] Realistic time estimate
- [ ] Prerequisites section lists all requirements
- [ ] Sequential `{{< steps >}}` using `{{< step >}}` shortcodes
- [ ] Use `{{< tabs >}}` for variants (resource types, platforms, options)
- [ ] Every command can be copy-pasted and works
- [ ] Expected output shown after commands hidden in `<details>` blocks
- [ ] Success indicators after major steps
- [ ] "What you've learned" summary
- [ ] Troubleshooting for tutorial-specific issues
- [ ] Working `relref` links to Concepts for "why" questions
