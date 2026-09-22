---
title: "Sign"
description: "Understand how OCM signs component versions and how consumers verify their integrity."
weight: 20
sidebar:
  collapsed: false
---

Signing is the second step in the OCM lifecycle: it attaches a cryptographic proof to a component
version so that any consumer can verify its integrity and origin after transfer. OCM stores
signatures inside the component descriptor so they travel with the component unchanged, and
supports multiple trust models to match different key management practices.
