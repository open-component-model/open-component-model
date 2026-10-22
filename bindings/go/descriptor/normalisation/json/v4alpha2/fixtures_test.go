package v4alpha2_test

// Known-answer fixtures for jsonNormalisation/v4alpha2.
//
// input is the stored component descriptor as readable v2 YAML. The v3Norm /
// v3Digest values are the byte-exact output of legacy ocm's
// jsonNormalisation/v3 for that same input — produced by
// compdesc.Normalize(cd, compdesc.JsonNormalisationV3) in the ocm.software/ocm
// module, an INDEPENDENT oracle. Do not recompute them with this normaliser;
// that would be circular.
//
// v4alpha2 re-converges with v3 wherever v3 already did the right thing (signing
// label.version is signed; non-signing labels are filtered at component,
// resource and source positions), and deliberately diverges only on component
// references, which v3 left unfiltered because its rule was keyed "references"
// while the JSON key is "componentReferences".

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
// version) and one non-signing label. v3 leaks BOTH labels unfiltered
// (referenceLabelsV3Norm). v4alpha2 filters them signing-only, keeping the
// version — exactly the v3 output minus the leaked non-signing label — so it
// diverges from v3 here by design.
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
	referenceLabelsV3Norm       = `{"component":{"componentReferences":[{"componentName":"acme.org/child","digest":{"hashAlgorithm":"SHA-256","normalisationAlgorithm":"jsonNormalisation/v4alpha1","value":"deadbeef"},"labels":[{"name":"ref-sig","signing":true,"value":"rsv","version":"v5"},{"name":"ref-nosig","value":"rnv"}],"name":"ref","version":"1.0.0"}],"name":"acme.org/reference-labels","provider":{"name":"acme"},"resources":[],"sources":[],"version":"1.0.0"}}`
	referenceLabelsV4alpha2Norm = `{"component":{"componentReferences":[{"componentName":"acme.org/child","digest":{"hashAlgorithm":"SHA-256","normalisationAlgorithm":"jsonNormalisation/v4alpha1","value":"deadbeef"},"labels":[{"name":"ref-sig","signing":true,"value":"rsv","version":"v5"}],"name":"ref","version":"1.0.0"}],"name":"acme.org/reference-labels","provider":{"name":"acme"},"resources":[],"sources":[],"version":"1.0.0"}}`
)
