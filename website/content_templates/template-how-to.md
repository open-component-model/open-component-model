---
title: "TEMPLATE: How-to Title"
description: "A task-focused recipe: do X in OCM."
weight: 999
toc: true
---

## Goal

One sentence: what you will achieve.

## You'll end up with

- A concrete outcome (artifact/config/state)
- A verifiable success condition

**Estimated time:** ~X minutes

## Workflow (optional)

Include a diagram only if it clarifies the process. Keep it simple (3–5 nodes max).

```mermaid
flowchart LR
    A[Input] --> B[ocm command] --> C[Output]
```

One sentence explaining what this diagram shows.

See [Diagram Color Guide]({{< relref "content_templates/template-tutorial.md#diagram-color-guide" >}}) for consistent styling across all documentation.

## Prerequisites

- [OCM CLI]({{< relref "docs/getting-started/install.md" >}}) installed
- Access to the required repository/service
- Required credentials/keys available

{{< callout type="warning" >}}
⚠️ **Preconditions:** Mention anything that would cause data loss or unexpected changes.
{{< /callout >}}

## Steps

### Do the first thing

Brief explanation (1–2 sentences max). Link to concepts for "why"—don't explain inline.

```bash
ocm <command> <args>
```

{{< callout type="tip" >}}
**Handling variants:** If your how-to covers multiple approaches or platforms, use tabs to show alternatives:
{{< /callout >}}

{{< tabs "signing-methods" >}}
{{< tab "RSA Key" >}}
```bash
ocm sign cv --private-key private.key
```
{{< /tab >}}
{{< tab "Sigstore" >}}
```bash
ocm sign cv --sigstore
```
{{< /tab >}}
{{< /tabs >}}

You should see: `[specific success indicator]`.

Hide complex output behind a details block:
{{< details "Expected output">}}

```text
...
```
{{< /details >}}

### Do the next thing

If needed, show the minimal config:

```yaml
# Key fields only
key: value
required: field
```

### Do the last thing

Show the shortest check that proves success:

```bash
ocm <command> --check
```

You should see: `[expected output]`. This confirms your setup is correct.

{{< details "Expected output">}}

```text
OK ...
```
{{< /details >}}

## Troubleshooting

### Symptom: [Specific error message]

**Cause:** One sentence explaining why.

**Fix:**

```bash
# Fix command
...
```

### Symptom: [Another issue]

**Cause:** ...

**Fix:** ...

## Cleanup (optional)

Remove resources created:

```bash
# Cleanup commands
...
```

{{< callout type="warning" >}}
⚠️ This will delete [what will be deleted].
{{< /callout >}}

## Next steps

Add links to related guides where you DO or LEARN more about the topic.

- [Tutorial: <name>]({{< relref "docs/tutorials/<file>.md" >}})

## Related documentation

Add links to related concepts and references that explain the WHY and provide more details.

- [Concept: <name>]({{< relref "docs/concepts/<file>.md" >}})
- [Reference: <command>]({{< relref "docs/reference/<file>.md" >}})

---

## ✓ Before publishing

Make sure to comply with our [CONTRIBUTING guide](../CONTRIBUTING.md)
and ensure the following:

- [ ] Title starts with "How to..." or action verb (Configure/Deploy/Create/...)
- [ ] States the goal in the first paragraph
- [ ] Realistic time estimate
- [ ] Simple numbered lists (use `{{< steps >}}` only if 3+ complex steps)
- [ ] Success indicators after each step
- [ ] Links to concepts (never inline "why" explanations)
- [ ] Use `{{< tabs >}}` for variants (different approaches, platforms, configurations)
- [ ] Troubleshooting with symptom-cause-fix
- [ ] Working relref links
