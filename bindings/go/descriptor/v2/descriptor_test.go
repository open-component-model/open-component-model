package descriptor

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/yaml"
)

const jsonData = `
{
  "meta": {
    "schemaVersion": "v2"
  },
  "component": {
    "name": "github.com/weaveworks/weave-gitops",
    "version": "v1.0.0",
    "provider": "weaveworks",
    "labels": [
      {
        "name": "link-to-documentation",
        "value": "https://github.com/weaveworks/weave-gitops"
      }
    ],
    "repositoryContexts": [
      {
        "baseUrl": "ghcr.io",
        "componentNameMapping": "urlPath",
        "subPath": "phoban01/ocm",
        "type": "OCIRegistry"
      }
    ],
    "resources": [
      {
        "name": "image",
        "relation": "external",
        "type": "ociImage",
        "version": "v0.14.1",
        "access": {
          "type": "ociArtifact",
          "imageReference": "ghcr.io/weaveworks/wego-app:v0.14.1"
        },
        "digest": {
          "hashAlgorithm": "SHA-256",
          "normalisationAlgorithm": "ociArtifactDigest/v1",
          "value": "efa2b9980ca2de65dc5a0c8cc05638b1a4b4ce8f6972dc08d0e805e5563ba5bb"
        }
      }
    ],
    "sources": [
      {
        "name": "weave-gitops",
        "type": "git",
        "version": "v0.14.1",
        "access": {
          "commit": "727513969553bfcc603e1c0ae1a75d79e4132b58",
          "ref": "refs/tags/v0.14.1",
          "repoUrl": "github.com/weaveworks/weave-gitops",
          "type": "gitHub"
        }
      }
    ],
    "componentReferences": [
      {
        "name": "prometheus",
        "version": "v1.0.0",
        "componentName": "cncf.io/prometheus",
        "digest": {
          "hashAlgorithm": "SHA-256",
          "normalisationAlgorithm": "jsonNormalisation/v1",
          "value": "04eb20b6fd942860325caf7f4415d1acf287a1aabd9e4827719328ba25d6f801"
        }
      }
    ]
  },
  "signatures": [
    {
      "name": "ww-dev",
      "digest": {
        "hashAlgorithm": "SHA-256",
        "normalisationAlgorithm": "jsonNormalisation/v1",
        "value": "4faff7822616305ecd09284d7c3e74a64f2269dcc524a9cdf0db4b592b8cee6a"
      },
      "signature": {
        "algorithm": "RSASSA-PSS",
        "mediaType": "application/vnd.ocm.signature.rsa",
        "value": "26468587671bdbd2166cf5f69829f090c10768511b15e804294fcb26e552654316c8f4851ed396f279ec99335e5f4b11cb043feb97f1f9a42115f4fda2d31ae8b481b7303b9a913d3a4b92d446fbee9ed487c93b09e513f3f68355040ec08454675e1f407422062abbd2681f70dd5488ad29020b30cfa7e001455c550458da96166bc3243c8426977d73352aface5323fb2b5a374e9c31b272a59c160b85631231c9fc2f23c032401b80fef937029a39111cee34470c61ae86cd4942553466411a5a116159fdcc10e50fe9360c5184028e72d1fe9c7315f26e15d7b4849f62d197501b8cc6b6f1b1391ecc2fc2fc0c1290d2554594505b25fa8f9bfb28c8df24"
      }
    }
  ]
}
`

const yamlData = `
meta:
  schemaVersion: v2
component:
  name: github.com/weaveworks/weave-gitops
  version: v1.0.0
  provider: weaveworks
  labels:
    - name: link-to-documentation
      value: https://github.com/weaveworks/weave-gitops
  repositoryContexts:
    - baseUrl: ghcr.io
      componentNameMapping: urlPath
      subPath: phoban01/ocm
      type: OCIRegistry
  resources:
    - name: image
      relation: external
      type: ociImage
      version: v0.14.1
      access:
        type: ociArtifact
        imageReference: ghcr.io/weaveworks/wego-app:v0.14.1
      digest:
        hashAlgorithm: SHA-256
        normalisationAlgorithm: ociArtifactDigest/v1
        value: efa2b9980ca2de65dc5a0c8cc05638b1a4b4ce8f6972dc08d0e805e5563ba5bb
  sources:
    - name: weave-gitops
      type: git
      version: v0.14.1
      access:
        commit: 727513969553bfcc603e1c0ae1a75d79e4132b58
        ref: refs/tags/v0.14.1
        repoUrl: github.com/weaveworks/weave-gitops
        type: gitHub
  componentReferences:
    - name: prometheus
      version: v1.0.0
      componentName: cncf.io/prometheus
      digest:
        hashAlgorithm: SHA-256
        normalisationAlgorithm: jsonNormalisation/v1
        value: 04eb20b6fd942860325caf7f4415d1acf287a1aabd9e4827719328ba25d6f801
signatures:
  - name: ww-dev
    digest:
      hashAlgorithm: SHA-256
      normalisationAlgorithm: jsonNormalisation/v1
      value: 4faff7822616305ecd09284d7c3e74a64f2269dcc524a9cdf0db4b592b8cee6a
    signature:
      algorithm: RSASSA-PSS
      mediaType: application/vnd.ocm.signature.rsa
      value: 26468587671bdbd2166cf5f69829f090c10768511b15e804294fcb26e552654316c8f4851ed396f279ec99335e5f4b11cb043feb97f1f9a42115f4fda2d31ae8b481b7303b9a913d3a4b92d446fbee9ed487c93b09e513f3f68355040ec08454675e1f407422062abbd2681f70dd5488ad29020b30cfa7e001455c550458da96166bc3243c8426977d73352aface5323fb2b5a374e9c31b272a59c160b85631231c9fc2f23c032401b80fef937029a39111cee34470c61ae86cd4942553466411a5a116159fdcc10e50fe9360c5184028e72d1fe9c7315f26e15d7b4849f62d197501b8cc6b6f1b1391ecc2fc2fc0c1290d2554594505b25fa8f9bfb28c8df24
`

func TestDescriptor_JSON(t *testing.T) {
	desc := Descriptor{}
	err := json.Unmarshal([]byte(jsonData), &desc)
	assert.Nil(t, err)

	assert.NotEmpty(t, desc.Component.Resources[0].GetIdentity())

	descData, err := json.Marshal(desc)
	assert.JSONEq(t, jsonData, string(descData))
	assert.Nil(t, err)
}

func TestDescriptor_YAML(t *testing.T) {
	desc := &Descriptor{}
	err := yaml.Unmarshal([]byte(yamlData), desc)
	assert.Nil(t, err)
	descData, err := yaml.Marshal(desc)
	assert.YAMLEq(t, yamlData, string(descData))
	assert.Nil(t, err)
	_ = descData
}
