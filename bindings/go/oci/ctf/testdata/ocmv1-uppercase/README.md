# OCM v1 uppercase version fixture

Generated with the OCM v1 CLI v0.52.0 (`darwin-arm64` release, SHA-256
`df6a5212e6008f274f29d6c49294542713324f4fb0bc7d22d5022a4ae1d25f7c`):

```sh
ocm add cv --repository <fixture-directory> --constructor constructor.yaml
```

The fixture contains the v1-produced CTF artifact index and blobs for
`ocm.software/ocmv1-uppercase:1.0.0-RC1`. The constructor was:

```yaml
name: ocm.software/ocmv1-uppercase
version: 1.0.0-RC1
provider:
  name: ocm.software
resources:
  - name: text
    version: 1.0.0-RC1
    type: plainText
    input:
      type: utf8
      text: uppercase prerelease
```
