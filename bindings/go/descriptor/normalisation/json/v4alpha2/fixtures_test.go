package v4alpha2_test

// Known-answer fixtures for jsonNormalisation/v4alpha2.
//
// input is the stored component descriptor as readable v2 YAML. The v3Norm /
// v3Digest values are the byte-exact output of legacy ocm's
// jsonNormalisation/v3 for that same input — produced by
// compdesc.Normalize(cd, compdesc.JsonNormalisationV3) in the ocm.software/ocm
// module, an INDEPENDENT oracle. Do not recompute them with this normaliser;
// that would be circular. The digests are the SHA-256 of those oracle bytes.
//
// v4alpha2 is the faithful re-implementation of v3: it must reproduce every
// oracle below byte-for-byte, including the unfiltered reference labels (v3
// emitted reference labels unfiltered and kept label.version; v4alpha2 matches
// both). v4alpha1 diverges wherever a label carries a version, because it drops
// label.version.

// deltaFree exercises neither delta: a signing label without a version plus a
// non-signing label, no reference labels. v3, v4alpha1 and v4alpha2 must all
// agree here.
const (
	deltaFreeInput = `
meta:
  schemaVersion: v2
component:
  name: acme.org/delta-free
  version: 1.0.0
  provider: acme
  repositoryContexts: []
  componentReferences: []
  sources: []
  labels:
    - name: comp-sig
      value: "cv"
      signing: true
    - name: comp-nosig
      value: "nv"
  resources:
    - name: res
      version: 1.0.0
      type: blob
      relation: local
      access:
        type: localBlob
        localReference: sha256:aaaa
        mediaType: application/octet-stream
      digest:
        hashAlgorithm: SHA-256
        normalisationAlgorithm: genericBlobDigest/v1
        value: aaaa
`
	deltaFreeV3Norm   = `{"component":{"componentReferences":[],"labels":[{"name":"comp-sig","signing":true,"value":"cv"}],"name":"acme.org/delta-free","provider":{"name":"acme"},"resources":[{"digest":{"hashAlgorithm":"SHA-256","normalisationAlgorithm":"genericBlobDigest/v1","value":"aaaa"},"name":"res","relation":"local","type":"blob","version":"1.0.0"}],"sources":[],"version":"1.0.0"}}`
	deltaFreeV3Digest = "2a3511daae5bc631a09c866f85cf86ac848972f0b20131edc8e54e0c115c2c53"
)

// versionLabels carries signing labels with a version at the component, resource
// and source positions (plus a non-signing label that must be dropped). v3 signs
// the version, so v4alpha2 must reproduce v3 byte-for-byte here; v4alpha1 drops
// the version and therefore diverges.
const (
	versionLabelsInput = `
meta:
  schemaVersion: v2
component:
  name: acme.org/version-labels
  version: 1.0.0
  provider: acme
  repositoryContexts: []
  componentReferences: []
  labels:
    - name: comp-sig
      value: "cv"
      signing: true
      version: v1
    - name: comp-nosig
      value: "nv"
  resources:
    - name: res
      version: 1.0.0
      type: blob
      relation: local
      access:
        type: localBlob
        localReference: sha256:aaaa
        mediaType: application/octet-stream
      digest:
        hashAlgorithm: SHA-256
        normalisationAlgorithm: genericBlobDigest/v1
        value: aaaa
      labels:
        - name: res-sig
          value: "rv"
          signing: true
          version: v3
        - name: res-nosig
          value: "nv"
  sources:
    - name: src
      version: 1.0.0
      type: git
      access:
        type: git
        repoUrl: github.com/acme/x
      labels:
        - name: src-sig
          value: "sv"
          signing: true
          version: v4
`
	versionLabelsV3Norm   = `{"component":{"componentReferences":[],"labels":[{"name":"comp-sig","signing":true,"value":"cv","version":"v1"}],"name":"acme.org/version-labels","provider":{"name":"acme"},"resources":[{"digest":{"hashAlgorithm":"SHA-256","normalisationAlgorithm":"genericBlobDigest/v1","value":"aaaa"},"labels":[{"name":"res-sig","signing":true,"value":"rv","version":"v3"}],"name":"res","relation":"local","type":"blob","version":"1.0.0"}],"sources":[{"labels":[{"name":"src-sig","signing":true,"value":"sv","version":"v4"}],"name":"src","type":"git","version":"1.0.0"}],"version":"1.0.0"}}`
	versionLabelsV3Digest = "97d6edffa6a0bc85795b4a5bfda0f4f36aae1dad86b34d80aa1cd12727f41561"
)

// referenceLabels carries a component reference with one signing label (with a
// version) and one non-signing label. v3 keeps BOTH labels unfiltered and signs
// the version. v4alpha2 reproduces that exactly (unfiltered, version kept),
// which is the behaviour v4alpha1 broke by stripping label.version. v4alpha3
// diverges here by filtering reference labels signing-only.
const (
	referenceLabelsInput = `
meta:
  schemaVersion: v2
component:
  name: acme.org/reference-labels
  version: 1.0.0
  provider: acme
  repositoryContexts: []
  resources: []
  sources: []
  componentReferences:
    - name: ref
      componentName: acme.org/child
      version: 1.0.0
      digest:
        hashAlgorithm: SHA-256
        normalisationAlgorithm: jsonNormalisation/v4alpha1
        value: deadbeef
      labels:
        - name: ref-sig
          value: "rsv"
          signing: true
          version: v5
        - name: ref-nosig
          value: "rnv"
`
	referenceLabelsV3Norm   = `{"component":{"componentReferences":[{"componentName":"acme.org/child","digest":{"hashAlgorithm":"SHA-256","normalisationAlgorithm":"jsonNormalisation/v4alpha1","value":"deadbeef"},"labels":[{"name":"ref-sig","signing":true,"value":"rsv","version":"v5"},{"name":"ref-nosig","value":"rnv"}],"name":"ref","version":"1.0.0"}],"name":"acme.org/reference-labels","provider":{"name":"acme"},"resources":[],"sources":[],"version":"1.0.0"}}`
	referenceLabelsV3Digest = "7d783015ce912c74cfe541f943adec9c0b9651fce9a79ec8e49d0288d09d9a87"
)
