---
title: "TOC Test: Tabs inside steps"
description: "Test page for step-items containing tab-groups — each step branches into tabbed variants."
icon: "🧪"
weight: 50
toc: true
---

## Overview

This page demonstrates the inverse of `steps-in-tabs`: an ordered step-list where individual step-items contain their own tab-group. The TOC must list every step's H3 heading and the H3 headings *inside* each step's tab-sections, with tab-aware filtering hiding non-active variants per step.

## Walkthrough

Work through the steps in order. Step 2 and Step 3 each contain a tab-group choosing between RSA and Sigstore variants of the same task.

{{< step-list >}}

{{< step-item >}}

### Install prerequisites

Install the OCM CLI and verify it is on your PATH. This step has no variants — both RSA and Sigstore users need the same tooling.

{{< step-item >}}

### Choose and configure your signer

Pick the signing technology you want to use, then prepare its configuration. Both options are supported throughout the rest of the tutorial.

{{< tab-group "step2" >}}

{{< tab-section "RSA" >}}

#### Configure RSA in step 2

Generate an RSA key pair locally and record the location of the private key file. The CLI will read it from this path during signing.

{{< tab-section "Sigstore" >}}

#### Configure Sigstore in step 2

Authenticate with your identity provider so the CLI can request short-lived OIDC tokens. No persistent key material is stored on disk.

{{< tab-group-end >}}

{{< step-item >}}

### Sign the component

Run the signing command. The exact command differs slightly between RSA and Sigstore — pick the matching tab.

{{< tab-group "step3" >}}

{{< tab-section "RSA" >}}

#### Sign with RSA in step 3

Execute the signing command pointing at your RSA private key. The signature is attached to the component descriptor.

{{< tab-section "Sigstore" >}}

#### Sign with Sigstore in step 3

Execute the signing command, which will trigger an OIDC flow. Sigstore issues a certificate binding your identity to the signature.

{{< tab-group-end >}}

{{< step-item >}}

### Verify the signature

Verify the signature with the matching public key (RSA) or with the Sigstore transparency log (Sigstore). This step has no tab variants because the verify command is unified.

{{< step-list-end >}}

## Cleanup

Remove any keys, tokens, or temporary signing artifacts created during this walkthrough.
