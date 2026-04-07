---
title: "Sign Component Versions"
description: "Learn how to sign component versions to ensure integrity and authenticity."
icon: "🔐"
weight: 25
toc: true
---

Component versions can be signed to ensure integrity along a transport chain.

Signing requires a key pair, a signature, and, optionally, an issuer, as well as an algorithm and a
name for the signature.

A component version can have multiple signatures with different names. A normalization of the
component version is used for signing. See [Signing Process and Normalization](https://github.com/open-component-model/ocm-spec/blob/main/doc/02-processing/03-signing-process.md) for more details.
Currently, only signing according to the
[RSA PKCS #1 v1.5 signature algorithm](https://datatracker.ietf.org/doc/html/rfc3447) is supported.

To follow the examples, one must follow the instructions from the section [Create a Component Version]({{< relref "docs/getting-started/create-component-version.md" >}}).

Create a key pair using the OCM CLI:

```shell
ocm create rsakeypair acme.priv
```

```shell
  created rsa key pair acme.priv[acme.pub]
```

This creates two files. One named `acme.priv` for the private key and for convenience one named
`acme.pub` for the public key.

Use the `sign componentversion` command to sign a component version:

```shell
ocm sign componentversion --signature acme-sig --private-key=acme.priv ${OCM_REPO}//${COMPONENT}:${VERSION}
```

```shell
  applying to version "github.com/acme/helloworld:1.0.0"[github.com/acme/helloworld:1.0.0]...
    resource 0:  "name"="mychart": digest SHA-256:...[ociArtifactDigest/v1]
    resource 1:  "name"="image": digest SHA-256:...[ociArtifactDigest/v1]
  successfully signed github.com/acme/helloworld:1.0.0 (digest SHA-256:...)
```

You can also sign a common transport archive before uploading to a component
repository:

```shell
ocm sign componentversion --signature acme-sig --private-key=acme.priv ${CTF_ARCHIVE}
```

```shell
  applying to version "github.com/acme.org/helloworld:1.0.0"[github.com/acme.org/helloworld:1.0.0]...
    resource 0:  "name"="mychart": digest SHA-256:...[ociArtifactDigest/v1]
    resource 1:  "name"="image": digest SHA-256:...[ociArtifactDigest/v1]
  successfully signed github.com/acme.org/helloworld:1.0.0 (digest SHA-256:...)
```

<details><summary>What happened?</summary>

Digests will be created for all described artifacts and referenced component versions. Then for the
top-level component versions, the component-version digests are signed. The signature and digests are
stored in the component descriptor(s):

```shell
jq . ${CTF_ARCHIVE}/artifact-index.json
```

```json
{
  "schemaVersion": 1,
  "artifacts": [
    {
      "repository": "component-descriptors/github.com/acme.org/helloworld",
      "tag": "1.0.0",
      "digest": "sha256:02b12782d66fc6504f0003bb11a8e2610ac8f3d616bc1a4545df17a6e9aca5c6"
    }
  ]
}
```

Beside the digests of the component descriptor layer, nothing has changed:

```shell
jq . ${CTF_ARCHIVE}/blobs/sha256.02b12782d66fc6504f0003bb11a8e2610ac8f3d616bc1a4545df17a6e9aca5c6
```

```json
{
  "schemaVersion": 2,
  "mediaType": "application/vnd.oci.image.manifest.v1+json",
  "config": {
    "mediaType": "application/vnd.ocm.software.component.config.v1+json",
    "digest": "sha256:38ba9898cb8d2c5ad34274549632836b391f5acc96268f0276d6857e87b97141",
    "size": 201
  },
  "layers": [
    {
      "mediaType": "application/vnd.ocm.software.component-descriptor.v2+yaml+tar",
      "digest": "sha256:c9705f0045f91c2cba49ce922dd65da27e66796e3a1fdc7a6fc01058357f2cd4",
      "size": 3584
    },
    {
      "mediaType": "application/vnd.oci.image.manifest.v1+tar+gzip",
      "digest": "sha256:125cf912d0f67b2b49e4170e684638a05a12f2fcfbdf3571e38a016273620b54",
      "size": 16119
    }
  ]
}
```

```shell
tar xvf ${CTF_ARCHIVE}/blobs/sha256.c9705f0045f91c2cba49ce922dd65da27e66796e3a1fdc7a6fc01058357f2cd4 -O - component-descriptor.yaml
```

```yaml
meta:
  schemaVersion: v2
component:
  name: github.com/acme.org/helloworld
  version: 1.0.0
  provider: acme.org
  componentReferences: []
  repositoryContexts: []
  resources:
  - access:
      localReference: sha256:125cf912d0f67b2b49e4170e684638a05a12f2fcfbdf3571e38a016273620b54
      mediaType: application/vnd.oci.image.manifest.v1+tar+gzip
      referenceName: github.com/acme.org/helloworld/podinfo:6.7.0
      type: localBlob
    digest:
      ...
    name: mychart
    relation: local
    type: helmChart
    version: 1.0.0
  - access:
      imageReference: gcr.io/google_containers/echoserver:1.10
      type: ociArtifact
    digest:
      ...
    name: image
    relation: external
    type: ociArtifact
    version: 1.0.0
  sources: []
signatures:
- digest:
    ...
  name: acme-sig
  signature:
    algorithm: RSASSA-PKCS1-V1_5
    mediaType: application/vnd.ocm.signature.rsa
    value: ...
```

</details>

## Signing with Certificates

The public key from the last example cannot be validated. This can be changed by using a certificate
instead of a pure public key. The certificate is signed by a CA. This ensures the authenticity of the
described public key. Additionally, the common name of the certificate is validated against the issuer
attribute of the signature stored in the component descriptor.

The following example creates a CA and signing certificates that are used to sign a component version.

Create the root CA:

```shell
ocm create rsakeypair --ca CN=certificate-authority root.priv
```

```shell
  created rsa key pair root.priv[root.cert]
```

Create the CA that is used to create signing certificates:

```shell
ocm create rsakeypair --ca CN=acme.org --ca-key root.priv --ca-cert root.cert ca.priv
```

```shell
  created rsa key pair ca.priv[ca.cert]
```

Create signing certificates from the CA:

```shell
ocm create rsakeypair CN=acme.org C=DE --ca-key ca.priv --ca-cert ca.cert --root-certs root.cert key.priv
```

```shell
  created rsa key pair key.priv[key.cert]
```

You can use additional attributes of the certificate like `O`, `OU` or `C`. See usage for details.
The certificate can be requested by any official certificate authority instead. It requires the usage types `x509.KeyUsageDigitalSignature` and `x509.ExtKeyUsageCodeSigning`.

For signing the component version you need to provide the issuer, then run:

```shell
ocm sign componentversion ${CTF_ARCHIVE} --private-key key.priv --public-key key.cert --ca-cert root.cert --signature acme.org --issuer CN=acme.org
```

```shell
  applying to version "github.com/acme.org/helloworld:1.0.0"[github.com/acme.org/helloworld:1.0.0]...
    resource 0:  "name"="mychart": digest SHA-256:...[ociArtifactDigest/v1]
    resource 1:  "name"="image": digest SHA-256:...[ociArtifactDigest/v1]
  successfully signed github.com/acme.org/helloworld:1.0.0 (digest SHA-256:...)
```

Now the issuer will be stored along the signature and will be checked when verifying with the certificate
instead of the public key.

## Signature Verification

You can verify a signed component version. Therefore, a public key or a certificate provided by the
signer is required. If a certificate is provided, it is validated according to its certificate chain.
If an official CA is used instead, you need the certificate of the used root CA.

If you followed the previous examples, you can verify the signature of a component version as follows:

```shell
ocm verify componentversions --signature acme-sig --public-key=acme.pub ${OCM_REPO}//${COMPONENT}:${VERSION}
```

```shell
  applying to version "github.com/acme/helloworld:1.0.0"[github.com/acme/helloworld:1.0.0]...
    resource 0:  "name"="mychart": digest SHA-256:...[ociArtifactDigest/v1]
    resource 1:  "name"="image": digest SHA-256:...[ociArtifactDigest/v1]
  successfully verified github.com/acme/helloworld:1.0.0 (digest SHA-256:...)
```

```shell
ocm verify component ${CTF_ARCHIVE} --ca-cert root.cert --issuer CN=acme.org
```

```shell
  applying to version "github.com/acme.org/helloworld:1.0.0"[github.com/acme.org/helloworld:1.0.0]...
    resource 0:  "name"="mychart": digest SHA-256:...[ociArtifactDigest/v1]
    resource 1:  "name"="image": digest SHA-256:...[ociArtifactDigest/v1]
  no public key found for signature "acme.org" -> extract key from signature
  successfully verified github.com/acme.org/helloworld:1.0.0 (digest SHA-256:...)
```
