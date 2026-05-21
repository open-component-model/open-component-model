---
title: "TOC Test: Steps inside tabs"
description: "Test page for tab-sections containing step-list blocks — each tab has its own steps."
icon: "🧪"
weight: 40
toc: true
---

## Overview

This page demonstrates tab-groups whose direct children are step-lists: each tab-section contains an ordered set of step-items, each step-item carrying its own H3 title. The TOC must show H3 step titles inside the tab-aware filtering — switching from RSA to Sigstore should swap which step titles are visible.

## Walkthrough

Follow the steps below for your chosen signing algorithm. Use the tabs to switch between RSA and Sigstore workflows.

{{< tab-group "signing" >}}

{{< tab-section "RSA" >}}

### Configure RSA

Prepare your RSA environment before signing. Ensure your RSA key pair is available and accessible from your working directory.

{{< step-list >}}

{{< step-item >}}

### Generate RSA keys

Create a new RSA key pair using your key generation tool. Save the private key securely and keep the public key for distribution to verifiers.

{{< step-item >}}

### Run sign with RSA

Execute the signing command with your RSA private key. The component is now signed and can be distributed with its RSA signature.

{{< step-list-end >}}

{{< tab-section "Sigstore" >}}

### Configure Sigstore

Prepare your Sigstore environment before signing. Verify that your identity (email or GitHub account) is accessible for OIDC verification.

{{< step-list >}}

{{< step-item >}}

### Acquire Sigstore token

Authenticate with your identity provider to obtain an OIDC token. This token proves your identity to the Sigstore infrastructure.

{{< step-item >}}

### Run sign with Sigstore

Execute the signing command with your OIDC token. Sigstore issues a certificate binding your identity to the signature.

{{< step-list-end >}}

{{< tab-group-end >}}

## Cleanup

After testing the signing workflows, remove any generated keys, tokens, or temporary artifacts from your working directory.
