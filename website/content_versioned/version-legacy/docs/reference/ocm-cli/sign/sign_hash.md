---
title: hash
name: sign hash
url: docs/reference/ocm-cli/sign/hash/
draft: false
weight: 55
toc: true
sidebar:
  collapsed: true
---
### Usage

```
ocm sign hash <private key file> <hash> [<issuer>]
```

### Options

```
  -S, --algorithm string      signature algorithm (default "RSASSA-PKCS1-V1_5")
      --ca-cert stringArray   additional root certificate authorities (for signing certificates)
  -h, --help                  help for hash
      --publicKey string      public key certificate file
      --rootCerts string      root certificates file (deprecated)
```

### Description


Print the signature for a dedicated digest value.
	

### Examples

```

$ ocm sign hash key.priv SHA-256:810ff2fb242a5dee4220f2cb0e6a519891fb67f2f828a6cab4ef8894633b1f50

```

### See Also

* [ocm sign]({{< relref "docs/reference/ocm-cli/sign/_index.md" >}})	 &mdash; Sign components or hashes

