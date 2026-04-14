---
title: config
name: get config
url: docs/reference/ocm-cli/get/config/
draft: false
weight: 55
toc: true
sidebar:
  collapsed: true
---
### Usage

```
ocm get config <options>
```

### Options

```
  -h, --help             help for config
  -O, --outfile string   output file or directory
  -o, --output string    output mode (JSON, json, yaml)
```

### Description


Evaluate the command line arguments and all explicitly
or implicitly used configuration files and provide
a single configuration object.


With the option <code>--output</code> the output mode can be selected.
The following modes are supported:
  - <code></code> (default)
  - <code>JSON</code>
  - <code>json</code>
  - <code>yaml</code>


### See Also

* [ocm get]({{< relref "docs/reference/ocm-cli/get/_index.md" >}})	 &mdash; Get information about artifacts and components

