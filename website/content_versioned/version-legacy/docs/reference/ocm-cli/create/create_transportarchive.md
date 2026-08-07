---
title: transportarchive
name: create transportarchive
url: docs/reference/ocm-cli/create/transportarchive/
draft: false
weight: 55
toc: true
sidebar:
  collapsed: true
---
### Usage

```
ocm create transportarchive [<options>] <path>
```

### Options

```
  -f, --force         remove existing content
  -h, --help          help for transportarchive
  -t, --type string   archive format (directory, tar, tgz) (default "directory")
```

### Description


Create a new empty OCM/OCI transport archive. This might be either a directory prepared
to host artifact content or a tar/tgz file.


### See Also

* [ocm create]({{< relref "docs/reference/ocm-cli/create/_index.md" >}})	 &mdash; Create transport or component archive

