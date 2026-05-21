---
title: "TOC Test: Marker tabs (flat)"
description: "Test page for tab-group with flat marker tabs to verify TOC rendering and tab switching."
icon: "🧪"
weight: 20
toc: true
---

## Overview

This page demonstrates a single tab-group with two tab-sections, each containing multiple H3 headings. The TOC should show page-level H2s (Overview, Walkthrough, Cleanup) with H3s nested under Walkthrough, organized by tab content. Switching tabs should keep the page structure intact without affecting the TOC.

## Walkthrough

This section contains the main workflow split into two algorithms. Use the tabs below to switch between RSA and Sigstore approaches.

{{< tab-group "demo" >}}

{{< tab-section "RSA" >}}

### Configure RSA

To use RSA signing, first generate your RSA key pair. Store the private key securely and distribute only the public key to verifiers. RSA provides strong cryptographic guarantees but requires key management infrastructure.

### Sign with RSA

With your RSA key pair in place, you can now sign components. Load the private key and execute the signing command. The resulting signature can be verified using the corresponding public key.

{{< tab-section "Sigstore" >}}

### Configure Sigstore

Sigstore provides a keyless signing model using identity verification. Set up your Sigstore configuration to use your email or GitHub identity. This eliminates the need to manage long-lived keys.

### Sign with Sigstore

Sign your component using Sigstore. The system will verify your identity and issue a short-lived certificate. The signature includes proof of identity verification.

{{< tab-group-end >}}

## Cleanup

After testing, remove any signing keys or credentials you generated. Clean up any test artifacts created during the signing process.
