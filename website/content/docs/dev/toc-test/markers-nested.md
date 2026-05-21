---
title: "TOC Test: Nested marker tabs"
description: "Test page for nested tab-groups to verify TOC rendering with multi-level tab structures."
icon: "🧪"
weight: 30
toc: true
---

## Overview

This page demonstrates nested tab-groups: an outer tab-group contains an inner tab-group within each of its sections. The TOC should correctly represent the nested structure, showing page-level H2s and section-specific H3s with their nested content properly indented.

## Pick algorithm

Choose your preferred signing algorithm from the tabs below. Each algorithm has its own configuration and signing workflows.

{{< tab-group "outer" >}}

{{< tab-section "RSA" >}}

### RSA introduction

RSA is a traditional public-key cryptography algorithm. It provides well-understood security properties and is widely supported across tools and platforms. RSA requires you to manage and protect private keys.

{{< tab-group "inner-rsa" >}}

{{< tab-section "Local" >}}

### RSA local config

For local RSA setup, generate your key pair on your development machine. Store the private key in a secure location on your filesystem, protected by appropriate file permissions.

{{< tab-section "Remote" >}}

### RSA remote config

For remote RSA setup, use a key management service or Hardware Security Module (HSM) to store your private key. This approach improves security by reducing exposure of the private key.

{{< tab-group-end >}}

### After-RSA notes

Once you complete RSA configuration (either local or remote), your RSA signing workflow is ready. All RSA signatures will use the configured key material.

{{< tab-section "Sigstore" >}}

### Sigstore introduction

Sigstore provides a modern keyless signing approach using OpenID Connect. Signatures include cryptographic proof of identity, eliminating the need for long-lived key management.

{{< tab-group "inner-sig" >}}

{{< tab-section "Local" >}}

### Sigstore local config

For local Sigstore setup, ensure your development environment can reach the Sigstore infrastructure and supports OIDC flows. Typically this means having your email or GitHub account accessible.

{{< tab-section "Remote" >}}

### Sigstore remote config

For remote Sigstore setup, configure your CI/CD pipeline to use Sigstore with the platform's native identity (GitHub Actions, GitLab CI, etc.). The identity is automatically bound to each signature.

{{< tab-group-end >}}

### After-Sigstore notes

With Sigstore configuration complete (local or remote), your keyless signing is ready. Signatures automatically include cryptographic proof of your identity.

{{< tab-group-end >}}

## Conclusion

You have now explored both RSA and Sigstore approaches with their local and remote configuration options. Choose the approach that best fits your security model and deployment infrastructure.
