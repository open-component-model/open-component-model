---
title: cache
name: clean cache
url: docs/reference/ocm-cli/clean/cache/
draft: false
weight: 55
toc: true
sidebar:
  collapsed: true
---
### Usage

```
ocm clean cache [<options>]
```

### Options

```
  -b, --before string   time since last usage
  -s, --dry-run         show size to be removed
  -h, --help            help for cache
```

### Description


Cleanup all blobs stored in oci blob cache (if given).
	

### Examples

```

$ ocm clean cache

```

### See Also

* [ocm clean]({{< relref "docs/reference/ocm-cli/clean/_index.md" >}})	 &mdash; Cleanup/re-organize elements

