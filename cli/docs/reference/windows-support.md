---
title: Windows Support
description: Information about Windows platform support for the OCM CLI.
---

## Windows Support

Windows support for the OCM CLI is currently **best-effort** and not guaranteed.

While the CLI is designed to handle Windows-specific conventions such as drive-letter paths
(e.g., `C:\path\to\archive`) and backslash path separators, there is no dedicated Windows CI
infrastructure in place to continuously validate these code paths.

### What this means

- Windows builds are cross-compiled and checked for compilation correctness.
- Windows-specific logic (such as path detection and normalization) might tested via simulated
  OS behavior on non-Windows runners.
- There is no runtime testing on actual Windows environments in CI.
- Bugs specific to Windows runtime behavior may go undetected until reported.

### Reporting issues

If you encounter a Windows-specific issue, please report it at
[github.com/open-component-model/open-component-model/issues](https://github.com/open-component-model/open-component-model/issues).
