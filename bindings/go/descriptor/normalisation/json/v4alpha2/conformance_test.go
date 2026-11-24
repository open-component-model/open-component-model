package v4alpha2_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha2"
	"ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
)

// TestConformance pins v4alpha2 against a real legacy jsonNormalisation/v3
// component descriptor. Because v4alpha2 is the v3 alias target
// (algorithms.LegacyV3 resolves here), its output must reproduce the recorded
// v3 oracle byte-for-byte. The fixture carries only non-signing labels and no
// reference labels, so v4alpha1, v4alpha2 and v3 all agree: this locks that the
// alias target stays faithful to a real-world v3 component.
func TestConformance(t *testing.T) {
	r := require.New(t)

	var desc descriptorv2.Descriptor
	r.NoError(yaml.Unmarshal([]byte(ocmCLI0270Input), &desc), "failed to unmarshal YAML")
	rt, err := runtime.ConvertFromV2(&desc)
	r.NoError(err, "failed to convert descriptor")

	got, err := normalisation.Normalise(rt, v4alpha2.Algorithm)
	r.NoError(err, "failed to normalise descriptor")

	r.Equal(ocmCLI0270V3Norm, string(got), //nolint:testifylint // byte-exact comparison, JSONEq would mask encoding differences
		"v4alpha2 must reproduce the legacy v3 oracle byte-for-byte")
}

// The OCM CLI 0.27.0 component version. The input is
//
//	ocm get cv ghcr.io/open-component-model/ocm//ocm.software/ocmcli:0.27.0 -oyaml
//
// and the v3 oracle, produced by legacy ocm (an independent implementation),
//
//	ocm hash cv ghcr.io/open-component-model/ocm//ocm.software/ocmcli:0.27.0 --normalization jsonNormalisation/v3 -onorm
const (
	ocmCLI0270Input = `---
component:
  componentReferences: []
  creationTime: "2025-07-28T11:40:51Z"
  name: ocm.software/ocmcli
  provider: ocm.software
  repositoryContexts:
    - baseUrl: ghcr.io
      componentNameMapping: urlPath
      subPath: open-component-model/ocm
      type: OCIRegistry
  resources:
    - access:
        localReference: sha256:64b586a57294adc5749324d0574df23499555de5168b4b1f1bd7ce9b06e2d49f
        mediaType: application/octet-stream
        type: localBlob
      digest:
        hashAlgorithm: SHA-256
        normalisationAlgorithm: genericBlobDigest/v1
        value: 64b586a57294adc5749324d0574df23499555de5168b4b1f1bd7ce9b06e2d49f
      extraIdentity:
        architecture: amd64
        os: windows
      labels:
        - name: downloadName
          value: ocm
      name: ocmcli
      relation: local
      type: executable
      version: 0.27.0
    - access:
        localReference: sha256:f038414e1ed095535e69b06c712e3c1a26f3fc399419721fdc8bf3c89ca499dd
        mediaType: application/octet-stream
        type: localBlob
      digest:
        hashAlgorithm: SHA-256
        normalisationAlgorithm: genericBlobDigest/v1
        value: f038414e1ed095535e69b06c712e3c1a26f3fc399419721fdc8bf3c89ca499dd
      extraIdentity:
        architecture: arm64
        os: darwin
      labels:
        - name: downloadName
          value: ocm
      name: ocmcli
      relation: local
      type: executable
      version: 0.27.0
    - access:
        localReference: sha256:6e244b50ce871e97b93c316907d75984d9828e6a55fb29e881495f6fd824ed15
        mediaType: application/octet-stream
        type: localBlob
      digest:
        hashAlgorithm: SHA-256
        normalisationAlgorithm: genericBlobDigest/v1
        value: 6e244b50ce871e97b93c316907d75984d9828e6a55fb29e881495f6fd824ed15
      extraIdentity:
        architecture: amd64
        os: darwin
      labels:
        - name: downloadName
          value: ocm
      name: ocmcli
      relation: local
      type: executable
      version: 0.27.0
    - access:
        localReference: sha256:1fb6cf9c9283497621ca7a30e74df64848c0d93fd74c953e700e8bb6b4054203
        mediaType: application/octet-stream
        type: localBlob
      digest:
        hashAlgorithm: SHA-256
        normalisationAlgorithm: genericBlobDigest/v1
        value: 1fb6cf9c9283497621ca7a30e74df64848c0d93fd74c953e700e8bb6b4054203
      extraIdentity:
        architecture: amd64
        os: linux
      labels:
        - name: downloadName
          value: ocm
      name: ocmcli
      relation: local
      type: executable
      version: 0.27.0
    - access:
        localReference: sha256:90d3a82557315ce9798075e769d52a8ea4352b4009526c7dd9b593f10fba4d77
        mediaType: application/octet-stream
        type: localBlob
      digest:
        hashAlgorithm: SHA-256
        normalisationAlgorithm: genericBlobDigest/v1
        value: 90d3a82557315ce9798075e769d52a8ea4352b4009526c7dd9b593f10fba4d77
      extraIdentity:
        architecture: arm64
        os: linux
      labels:
        - name: downloadName
          value: ocm
      name: ocmcli
      relation: local
      type: executable
      version: 0.27.0
    - access:
        imageReference: ghcr.io/open-component-model/ocm/ocm.software/ocmcli/ocmcli-image:0.27.0@sha256:b91854cb7e2b73decd197ad1408cb09fe6a2af8b289eda9030eb75a722ffef63
        type: ociArtifact
      digest:
        hashAlgorithm: SHA-256
        normalisationAlgorithm: ociArtifactDigest/v1
        value: b91854cb7e2b73decd197ad1408cb09fe6a2af8b289eda9030eb75a722ffef63
      name: ocmcli-image
      relation: local
      type: ociImage
      version: 0.27.0
  sources:
    - access:
        commit: 3ff8ed0e7386aa6fe19dcbbb8955f34a88745fd0
        repoUrl: github.com/open-component-model/ocm
        type: github
      name: source
      type: filesytem
      version: 0.27.0
  version: 0.27.0
meta:
  schemaVersion: v2`

	ocmCLI0270V3Norm = `{"component":{"componentReferences":[],"creationTime":"2025-07-28T11:40:51Z","name":"ocm.software/ocmcli","provider":{"name":"ocm.software"},"resources":[{"digest":{"hashAlgorithm":"SHA-256","normalisationAlgorithm":"genericBlobDigest/v1","value":"64b586a57294adc5749324d0574df23499555de5168b4b1f1bd7ce9b06e2d49f"},"extraIdentity":{"architecture":"amd64","os":"windows"},"name":"ocmcli","relation":"local","type":"executable","version":"0.27.0"},{"digest":{"hashAlgorithm":"SHA-256","normalisationAlgorithm":"genericBlobDigest/v1","value":"f038414e1ed095535e69b06c712e3c1a26f3fc399419721fdc8bf3c89ca499dd"},"extraIdentity":{"architecture":"arm64","os":"darwin"},"name":"ocmcli","relation":"local","type":"executable","version":"0.27.0"},{"digest":{"hashAlgorithm":"SHA-256","normalisationAlgorithm":"genericBlobDigest/v1","value":"6e244b50ce871e97b93c316907d75984d9828e6a55fb29e881495f6fd824ed15"},"extraIdentity":{"architecture":"amd64","os":"darwin"},"name":"ocmcli","relation":"local","type":"executable","version":"0.27.0"},{"digest":{"hashAlgorithm":"SHA-256","normalisationAlgorithm":"genericBlobDigest/v1","value":"1fb6cf9c9283497621ca7a30e74df64848c0d93fd74c953e700e8bb6b4054203"},"extraIdentity":{"architecture":"amd64","os":"linux"},"name":"ocmcli","relation":"local","type":"executable","version":"0.27.0"},{"digest":{"hashAlgorithm":"SHA-256","normalisationAlgorithm":"genericBlobDigest/v1","value":"90d3a82557315ce9798075e769d52a8ea4352b4009526c7dd9b593f10fba4d77"},"extraIdentity":{"architecture":"arm64","os":"linux"},"name":"ocmcli","relation":"local","type":"executable","version":"0.27.0"},{"digest":{"hashAlgorithm":"SHA-256","normalisationAlgorithm":"ociArtifactDigest/v1","value":"b91854cb7e2b73decd197ad1408cb09fe6a2af8b289eda9030eb75a722ffef63"},"name":"ocmcli-image","relation":"local","type":"ociImage","version":"0.27.0"}],"sources":[{"name":"source","type":"filesytem","version":"0.27.0"}],"version":"0.27.0"}}`
)
