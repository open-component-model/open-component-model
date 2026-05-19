---
title: "TOC Test: Marker tabs with step blocks"
description: "Test page for tab-groups containing step blocks to verify complex nested TOC structures."
icon: "🧪"
weight: 70
toc: true
---

## Overview

This page demonstrates tab-groups containing step blocks. Each tab-section has both H3 section headings and a `step-list` block whose title is also a TOC-visible H3.

## Walkthrough

Follow the steps below for your chosen signing algorithm. Use the tabs to switch between RSA and Sigstore workflows.

{{< tab-group "wts" >}}

{{< tab-section "RSA" >}}

### Configure RSA

Prepare your RSA environment before signing. Ensure your RSA key pair is available and accessible from your working directory.

{{% step-list "Sign the component (RSA)" %}}

{{< step-item >}}

#### Generate keys

Create a new RSA key pair using your key generation tool. Save the private key securely and keep the public key for distribution to verifiers.

{{< /step-item >}}

{{< step-item >}}

#### Run sign

Execute the signing command with your RSA private key. The component is now signed and can be distributed with its RSA signature.

{{< /step-item >}}

{{% step-list-end %}}

{{< tab-section "Sigstore" >}}

### Configure Sigstore

Prepare your Sigstore environment before signing. Verify that your identity (email or GitHub account) is accessible for OIDC verification.

{{% step-list "Sign the component (Sigstore)" %}}

{{< step-item >}}

#### Acquire token

Authenticate with your identity provider to obtain an OIDC token. This token proves your identity to the Sigstore infrastructure.

{{< /step-item >}}

{{< step-item >}}

#### Run sign

Execute the signing command with your OIDC token. Sigstore issues a certificate binding your identity to the signature.

{{< /step-item >}}

{{% step-list-end %}}

{{< tab-group-end >}}

## Cleanup

After testing the signing workflows, remove any generated keys, tokens, or temporary artifacts from your working directory.
