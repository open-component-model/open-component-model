---
title: verified
name: get verified
url: docs/reference/ocm-cli/get/verified/
draft: false
weight: 55
toc: true
sidebar:
  collapsed: true
---
### Usage

```
ocm get verified [<options>] {<component / version}
```

### Options

```
  -h, --help               help for verified
  -o, --output string      output mode (JSON, json, wide, yaml)
  -s, --sort stringArray   sort fields
      --verified string    verified file (default "~/.ocm/verified")
```

### Description


Get lists remembered verified component versions.


With the option <code>--output</code> the output mode can be selected.
The following modes are supported:
  - <code></code> (default)
  - <code>JSON</code>
  - <code>json</code>
  - <code>wide</code>
  - <code>yaml</code>


### Examples

```

$ ocm get verified
$ ocm get verified -f verified.yaml acme.org/component -o yaml

```

### See Also

* [ocm get]({{< relref "docs/reference/ocm-cli/get/_index.md" >}})	 &mdash; Get information about artifacts and components

