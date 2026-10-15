package v1_test

// Known-answer fixtures transcribed from the cc-utils anchor (ocm/sign.py).
// input is the cd as readable YAML; norm is the cc-utils reference normalised
// bytes (byte-exact) and digest is sha256(norm). Do not recompute expected
// values with this normaliser — that would be circular.

const (
	input_01_minimal_oci = `
component:
  componentReferences: []
  creationTime: null
  labels: []
  name: acme.org/minimal
  provider: ACME
  repositoryContexts: []
  resources:
  - access:
      imageReference: registry.example/img@sha256:aa
      type: ociRegistry
    digest:
      hashAlgorithm: SHA-256
      normalisationAlgorithm: ociArtifactDigest/v1
      value: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
    extraIdentity: {}
    labels: []
    name: img
    relation: external
    srcRefs: []
    type: ociImage
    version: 1.0.0
  sources: []
  version: 1.0.0
meta:
  schemaVersion: v2
nestedDigests: []
signatures: []
`
	norm_01_minimal_oci   = `[{"component":[{"componentReferences":[]},{"name":"acme.org/minimal"},{"provider":"ACME"},{"resources":[[{"digest":[{"hashAlgorithm":"SHA-256"},{"normalisationAlgorithm":"ociArtifactDigest/v1"},{"value":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]},{"extraIdentity":null},{"name":"img"},{"relation":"external"},{"type":"ociImage"},{"version":"1.0.0"}]]},{"version":"1.0.0"}]},{"meta":[{"schemaVersion":"v2"}]}]`
	digest_01_minimal_oci = `f71d1338bab2ebc5135297a961449607203f2b1adecf6f7244a09aad0957188e`
)

const (
	input_02_localblob_generic = `
component:
  componentReferences: []
  creationTime: null
  labels: []
  name: acme.org/localblob
  provider: ACME
  repositoryContexts: []
  resources:
  - access:
      globalAccess: null
      localReference: sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
      mediaType: application/vnd.oci.image.manifest.v1+tar+gzip
      referenceName: null
      size: null
      type: localBlob/v1
    digest:
      hashAlgorithm: SHA-256
      normalisationAlgorithm: genericBlobDigest/v1
      value: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
    extraIdentity: {}
    labels: []
    name: chart
    relation: local
    srcRefs: []
    type: helmChart
    version: 1.0.0
  sources: []
  version: 1.0.0
meta:
  schemaVersion: v2
nestedDigests: []
signatures: []
`
	norm_02_localblob_generic   = `[{"component":[{"componentReferences":[]},{"name":"acme.org/localblob"},{"provider":"ACME"},{"resources":[[{"digest":[{"hashAlgorithm":"SHA-256"},{"normalisationAlgorithm":"genericBlobDigest/v1"},{"value":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}]},{"extraIdentity":null},{"name":"chart"},{"relation":"local"},{"type":"helmChart"},{"version":"1.0.0"}]]},{"version":"1.0.0"}]},{"meta":[{"schemaVersion":"v2"}]}]`
	digest_02_localblob_generic = `caf039827990dfb2256972b33220894433eb0d6412c6bca075220c555620c614`
)

const (
	input_03_exclude_from_signature = `
component:
  componentReferences: []
  creationTime: null
  labels: []
  name: acme.org/excluded
  provider: ACME
  repositoryContexts: []
  resources:
  - access:
      bucket: b
      key: k
      mediaType: null
      region: null
      type: s3
    digest:
      hashAlgorithm: NO-DIGEST
      normalisationAlgorithm: EXCLUDE-FROM-SIGNATURE
      value: NO-DIGEST
    extraIdentity: {}
    labels: []
    name: blob
    relation: external
    srcRefs: []
    type: blob
    version: 1.0.0
  sources: []
  version: 1.0.0
meta:
  schemaVersion: v2
nestedDigests: []
signatures: []
`
	norm_03_exclude_from_signature   = `[{"component":[{"componentReferences":[]},{"name":"acme.org/excluded"},{"provider":"ACME"},{"resources":[[{"digest":[{"hashAlgorithm":"NO-DIGEST"},{"normalisationAlgorithm":"EXCLUDE-FROM-SIGNATURE"},{"value":"NO-DIGEST"}]},{"extraIdentity":null},{"name":"blob"},{"relation":"external"},{"type":"blob"},{"version":"1.0.0"}]]},{"version":"1.0.0"}]},{"meta":[{"schemaVersion":"v2"}]}]`
	digest_03_exclude_from_signature = `36d909f5e5f0f5962cbb91483cea4af330c07fd04b671efc9d2b80056196f9e5`
)

const (
	input_04_labels = `
component:
  componentReferences: []
  creationTime: null
  labels: []
  name: acme.org/labels
  provider: ACME
  repositoryContexts: []
  resources:
  - access:
      imageReference: registry.example/img@sha256:cc
      type: ociRegistry
    digest:
      hashAlgorithm: SHA-256
      normalisationAlgorithm: ociArtifactDigest/v1
      value: cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
    extraIdentity: {}
    labels:
    - name: signed-no-ver
      signing: true
      value: x
      version: null
    - name: signed-ver
      signing: true
      value:
        k: v
      version: v1
    - name: unsigned
      signing: false
      value: drop-me
      version: null
    name: img
    relation: external
    srcRefs: []
    type: ociImage
    version: 1.0.0
  sources: []
  version: 1.0.0
meta:
  schemaVersion: v2
nestedDigests: []
signatures: []
`
	norm_04_labels   = `[{"component":[{"componentReferences":[]},{"name":"acme.org/labels"},{"provider":"ACME"},{"resources":[[{"digest":[{"hashAlgorithm":"SHA-256"},{"normalisationAlgorithm":"ociArtifactDigest/v1"},{"value":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"}]},{"extraIdentity":null},{"labels":[[{"name":"signed-no-ver"},{"signing":true},{"value":"x"}],[{"name":"signed-ver"},{"signing":true},{"value":[{"k":"v"}]},{"version":"v1"}]]},{"name":"img"},{"relation":"external"},{"type":"ociImage"},{"version":"1.0.0"}]]},{"version":"1.0.0"}]},{"meta":[{"schemaVersion":"v2"}]}]`
	digest_04_labels = `9416f2fb670822bac47931510219e63a370ca68460764121f1f8ee2a6b582bcd`
)

const (
	input_05_duplicate_identity = `
component:
  componentReferences: []
  creationTime: null
  labels: []
  name: acme.org/dup
  provider: ACME
  repositoryContexts: []
  resources:
  - access:
      imageReference: registry.example/a@sha256:d1
      type: ociRegistry
    digest:
      hashAlgorithm: SHA-256
      normalisationAlgorithm: ociArtifactDigest/v1
      value: dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd
    extraIdentity: {}
    labels: []
    name: same
    relation: external
    srcRefs: []
    type: ociImage
    version: 1.0.0
  - access:
      imageReference: registry.example/a@sha256:d2
      type: ociRegistry
    digest:
      hashAlgorithm: SHA-256
      normalisationAlgorithm: ociArtifactDigest/v1
      value: eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee
    extraIdentity: {}
    labels: []
    name: same
    relation: external
    srcRefs: []
    type: ociImage
    version: 1.0.0
  sources: []
  version: 1.0.0
meta:
  schemaVersion: v2
nestedDigests: []
signatures: []
`
	norm_05_duplicate_identity   = `[{"component":[{"componentReferences":[]},{"name":"acme.org/dup"},{"provider":"ACME"},{"resources":[[{"digest":[{"hashAlgorithm":"SHA-256"},{"normalisationAlgorithm":"ociArtifactDigest/v1"},{"value":"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"}]},{"extraIdentity":[{"version":"1.0.0"}]},{"name":"same"},{"relation":"external"},{"type":"ociImage"},{"version":"1.0.0"}],[{"digest":[{"hashAlgorithm":"SHA-256"},{"normalisationAlgorithm":"ociArtifactDigest/v1"},{"value":"eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"}]},{"extraIdentity":null},{"name":"same"},{"relation":"external"},{"type":"ociImage"},{"version":"1.0.0"}]]},{"version":"1.0.0"}]},{"meta":[{"schemaVersion":"v2"}]}]`
	digest_05_duplicate_identity = `cc9675da131b504b06d74c233d1c8c669e4649fc1c5ae7044ddc9e250efe588b`
)

const (
	input_06_creationtime_rounding = `
component:
  componentReferences: []
  creationTime: "2026-06-19T07:16:30.600000+00:00"
  labels: []
  name: acme.org/ctime
  provider: ACME
  repositoryContexts: []
  resources:
  - access:
      imageReference: registry.example/img@sha256:ff
      type: ociRegistry
    digest:
      hashAlgorithm: SHA-256
      normalisationAlgorithm: ociArtifactDigest/v1
      value: ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff
    extraIdentity: {}
    labels: []
    name: img
    relation: external
    srcRefs: []
    type: ociImage
    version: 1.0.0
  sources: []
  version: 1.0.0
meta:
  schemaVersion: v2
nestedDigests: []
signatures: []
`
	norm_06_creationtime_rounding   = `[{"component":[{"componentReferences":[]},{"creationTime":"2026-06-19T07:16:31Z"},{"name":"acme.org/ctime"},{"provider":"ACME"},{"resources":[[{"digest":[{"hashAlgorithm":"SHA-256"},{"normalisationAlgorithm":"ociArtifactDigest/v1"},{"value":"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"}]},{"extraIdentity":null},{"name":"img"},{"relation":"external"},{"type":"ociImage"},{"version":"1.0.0"}]]},{"version":"1.0.0"}]},{"meta":[{"schemaVersion":"v2"}]}]`
	digest_06_creationtime_rounding = `fcca04060739c8bcf5f753ff596e69c5eeb69ad8e6c55c596715dbd13bf24c16`
)

const (
	input_07_child = `
component:
  componentReferences: []
  creationTime: null
  labels: []
  name: acme.org/child
  provider: ACME
  repositoryContexts: []
  resources:
  - access:
      imageReference: registry.example/child@sha256:11
      type: ociRegistry
    digest:
      hashAlgorithm: SHA-256
      normalisationAlgorithm: ociArtifactDigest/v1
      value: "1111111111111111111111111111111111111111111111111111111111111111"
    extraIdentity: {}
    labels: []
    name: cimg
    relation: external
    srcRefs: []
    type: ociImage
    version: 1.0.0
  sources: []
  version: 1.0.0
meta:
  schemaVersion: v2
nestedDigests: []
signatures: []
`
	norm_07_child   = `[{"component":[{"componentReferences":[]},{"name":"acme.org/child"},{"provider":"ACME"},{"resources":[[{"digest":[{"hashAlgorithm":"SHA-256"},{"normalisationAlgorithm":"ociArtifactDigest/v1"},{"value":"1111111111111111111111111111111111111111111111111111111111111111"}]},{"extraIdentity":null},{"name":"cimg"},{"relation":"external"},{"type":"ociImage"},{"version":"1.0.0"}]]},{"version":"1.0.0"}]},{"meta":[{"schemaVersion":"v2"}]}]`
	digest_07_child = `4518a3606aa1d257ff08d1760b84490ce2c741c1278217936e7abfe5135d2976`
)

const (
	input_07_root_with_reference = `
component:
  componentReferences:
  - componentName: acme.org/child
    digest:
      hashAlgorithm: SHA-256
      normalisationAlgorithm: jsonNormalisation/v1
      value: 4518a3606aa1d257ff08d1760b84490ce2c741c1278217936e7abfe5135d2976
    extraIdentity: {}
    labels: []
    name: child
    version: 1.0.0
  creationTime: null
  labels: []
  name: acme.org/root
  provider: ACME
  repositoryContexts: []
  resources:
  - access:
      imageReference: registry.example/root@sha256:22
      type: ociRegistry
    digest:
      hashAlgorithm: SHA-256
      normalisationAlgorithm: ociArtifactDigest/v1
      value: "2222222222222222222222222222222222222222222222222222222222222222"
    extraIdentity: {}
    labels: []
    name: rimg
    relation: external
    srcRefs: []
    type: ociImage
    version: 1.0.0
  sources: []
  version: 1.0.0
meta:
  schemaVersion: v2
nestedDigests: []
signatures: []
`
	norm_07_root_with_reference   = `[{"component":[{"componentReferences":[[{"componentName":"acme.org/child"},{"digest":[{"hashAlgorithm":"SHA-256"},{"normalisationAlgorithm":"jsonNormalisation/v1"},{"value":"4518a3606aa1d257ff08d1760b84490ce2c741c1278217936e7abfe5135d2976"}]},{"extraIdentity":null},{"name":"child"},{"version":"1.0.0"}]]},{"name":"acme.org/root"},{"provider":"ACME"},{"resources":[[{"digest":[{"hashAlgorithm":"SHA-256"},{"normalisationAlgorithm":"ociArtifactDigest/v1"},{"value":"2222222222222222222222222222222222222222222222222222222222222222"}]},{"extraIdentity":null},{"name":"rimg"},{"relation":"external"},{"type":"ociImage"},{"version":"1.0.0"}]]},{"version":"1.0.0"}]},{"meta":[{"schemaVersion":"v2"}]}]`
	digest_07_root_with_reference = `1518f5f3e68f341cd3a28570f9494e1729f007b47569efe9447c57eee973c2af`
)

const (
	input_08_full_signed = `
component:
  componentReferences:
  - componentName: acme.org/child
    digest:
      hashAlgorithm: SHA-256
      normalisationAlgorithm: jsonNormalisation/v1
      value: 4518a3606aa1d257ff08d1760b84490ce2c741c1278217936e7abfe5135d2976
    extraIdentity: {}
    labels: []
    name: child
    version: 1.0.0
  creationTime: "2026-06-19T07:16:30.600000+00:00"
  labels: []
  name: acme.org/full
  provider: ACME
  repositoryContexts: []
  resources:
  - access:
      imageReference: registry.example/oci@sha256:aa
      type: ociRegistry
    digest:
      hashAlgorithm: SHA-256
      normalisationAlgorithm: ociArtifactDigest/v1
      value: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
    extraIdentity: {}
    labels:
    - name: sig
      signing: true
      value: v
      version: null
    - name: nosig
      signing: false
      value: x
      version: null
    name: oci
    relation: external
    srcRefs: []
    type: ociImage
    version: 1.0.0
  - access:
      globalAccess: null
      localReference: sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
      mediaType: application/vnd.oci.image.manifest.v1+tar+gzip
      referenceName: null
      size: null
      type: localBlob/v1
    digest:
      hashAlgorithm: SHA-256
      normalisationAlgorithm: genericBlobDigest/v1
      value: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
    extraIdentity: {}
    labels: []
    name: chart
    relation: local
    srcRefs: []
    type: helmChart
    version: 1.0.0
  - access:
      bucket: b
      key: k
      mediaType: null
      region: null
      type: s3
    digest:
      hashAlgorithm: NO-DIGEST
      normalisationAlgorithm: EXCLUDE-FROM-SIGNATURE
      value: NO-DIGEST
    extraIdentity: {}
    labels: []
    name: blob
    relation: external
    srcRefs: []
    type: blob
    version: 1.0.0
  - access:
      imageReference: registry.example/a@sha256:d1
      type: ociRegistry
    digest:
      hashAlgorithm: SHA-256
      normalisationAlgorithm: ociArtifactDigest/v1
      value: dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd
    extraIdentity: {}
    labels: []
    name: same
    relation: external
    srcRefs: []
    type: ociImage
    version: 1.0.0
  - access:
      imageReference: registry.example/a@sha256:d2
      type: ociRegistry
    digest:
      hashAlgorithm: SHA-256
      normalisationAlgorithm: ociArtifactDigest/v1
      value: eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee
    extraIdentity: {}
    labels: []
    name: same
    relation: external
    srcRefs: []
    type: ociImage
    version: 1.0.0
  sources: []
  version: 1.0.0
meta:
  schemaVersion: v2
nestedDigests: []
signatures: []
`
	norm_08_full_signed   = `[{"component":[{"componentReferences":[[{"componentName":"acme.org/child"},{"digest":[{"hashAlgorithm":"SHA-256"},{"normalisationAlgorithm":"jsonNormalisation/v1"},{"value":"4518a3606aa1d257ff08d1760b84490ce2c741c1278217936e7abfe5135d2976"}]},{"extraIdentity":null},{"name":"child"},{"version":"1.0.0"}]]},{"creationTime":"2026-06-19T07:16:31Z"},{"name":"acme.org/full"},{"provider":"ACME"},{"resources":[[{"digest":[{"hashAlgorithm":"SHA-256"},{"normalisationAlgorithm":"ociArtifactDigest/v1"},{"value":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]},{"extraIdentity":null},{"labels":[[{"name":"sig"},{"signing":true},{"value":"v"}]]},{"name":"oci"},{"relation":"external"},{"type":"ociImage"},{"version":"1.0.0"}],[{"digest":[{"hashAlgorithm":"SHA-256"},{"normalisationAlgorithm":"genericBlobDigest/v1"},{"value":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}]},{"extraIdentity":null},{"name":"chart"},{"relation":"local"},{"type":"helmChart"},{"version":"1.0.0"}],[{"digest":[{"hashAlgorithm":"NO-DIGEST"},{"normalisationAlgorithm":"EXCLUDE-FROM-SIGNATURE"},{"value":"NO-DIGEST"}]},{"extraIdentity":null},{"name":"blob"},{"relation":"external"},{"type":"blob"},{"version":"1.0.0"}],[{"digest":[{"hashAlgorithm":"SHA-256"},{"normalisationAlgorithm":"ociArtifactDigest/v1"},{"value":"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"}]},{"extraIdentity":[{"version":"1.0.0"}]},{"name":"same"},{"relation":"external"},{"type":"ociImage"},{"version":"1.0.0"}],[{"digest":[{"hashAlgorithm":"SHA-256"},{"normalisationAlgorithm":"ociArtifactDigest/v1"},{"value":"eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"}]},{"extraIdentity":null},{"name":"same"},{"relation":"external"},{"type":"ociImage"},{"version":"1.0.0"}]]},{"version":"1.0.0"}]},{"meta":[{"schemaVersion":"v2"}]}]`
	digest_08_full_signed = `09fefed1884db76c829ba5f78ad315f26024c241121ebefd6be5aa079869a74f`
)

const (
	input_09_creationtime_offset_fraction = `
component:
  componentReferences: []
  creationTime: "2025-06-20T11:25:07.949163+00:00"
  labels: []
  name: acme.org/ctime-offset
  provider: ACME
  repositoryContexts: []
  resources:
  - access:
      imageReference: registry.example/img@sha256:99
      type: ociRegistry
    digest:
      hashAlgorithm: SHA-256
      normalisationAlgorithm: ociArtifactDigest/v1
      value: "9999999999999999999999999999999999999999999999999999999999999999"
    extraIdentity: {}
    labels: []
    name: img
    relation: external
    srcRefs: []
    type: ociImage
    version: 1.0.0
  sources: []
  version: 1.0.0
meta:
  schemaVersion: v2
nestedDigests: []
signatures: []
`
	norm_09_creationtime_offset_fraction   = `[{"component":[{"componentReferences":[]},{"creationTime":"2025-06-20T11:25:08Z"},{"name":"acme.org/ctime-offset"},{"provider":"ACME"},{"resources":[[{"digest":[{"hashAlgorithm":"SHA-256"},{"normalisationAlgorithm":"ociArtifactDigest/v1"},{"value":"9999999999999999999999999999999999999999999999999999999999999999"}]},{"extraIdentity":null},{"name":"img"},{"relation":"external"},{"type":"ociImage"},{"version":"1.0.0"}]]},{"version":"1.0.0"}]},{"meta":[{"schemaVersion":"v2"}]}]`
	digest_09_creationtime_offset_fraction = `af0029c875a1b5bbf39e99b34a643082cf4cc8caa93a75f11d4948bc963e8b48`
)

const (
	input_10_unicode_label = `
component:
  componentReferences: []
  creationTime: null
  labels: []
  name: acme.org/unicode
  provider: ACME
  repositoryContexts: []
  resources:
  - access:
      imageReference: registry.example/img@sha256:77
      type: ociRegistry
    digest:
      hashAlgorithm: SHA-256
      normalisationAlgorithm: ociArtifactDigest/v1
      value: "7777777777777777777777777777777777777777777777777777777777777777"
    extraIdentity: {}
    labels:
    - name: desc
      signing: true
      value: "café \U0001F600 <&>"
      version: null
    name: img
    relation: external
    srcRefs: []
    type: ociImage
    version: 1.0.0
  sources: []
  version: 1.0.0
meta:
  schemaVersion: v2
nestedDigests: []
signatures: []
`
	norm_10_unicode_label   = `[{"component":[{"componentReferences":[]},{"name":"acme.org/unicode"},{"provider":"ACME"},{"resources":[[{"digest":[{"hashAlgorithm":"SHA-256"},{"normalisationAlgorithm":"ociArtifactDigest/v1"},{"value":"7777777777777777777777777777777777777777777777777777777777777777"}]},{"extraIdentity":null},{"labels":[[{"name":"desc"},{"signing":true},{"value":"caf\u00e9 \ud83d\ude00 <&>"}]]},{"name":"img"},{"relation":"external"},{"type":"ociImage"},{"version":"1.0.0"}]]},{"version":"1.0.0"}]},{"meta":[{"schemaVersion":"v2"}]}]`
	digest_10_unicode_label = `5f181460957c670cd753457020cf09a00860e6251f31948bcceb2ab2d950f728`
)

const (
	input_11_dup_delimiter = `
component:
  componentReferences: []
  creationTime: null
  labels: []
  name: acme.org/dup-delim
  provider: ACME
  repositoryContexts: []
  resources:
  - access:
      imageReference: registry.example/a@sha256:d1
      type: ociRegistry
    digest:
      hashAlgorithm: SHA-256
      normalisationAlgorithm: ociArtifactDigest/v1
      value: dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd
    extraIdentity:
      aa: b;cc=dd
    labels: []
    name: res
    relation: external
    srcRefs: []
    type: ociImage
    version: 1.0.0
  - access:
      imageReference: registry.example/a@sha256:d2
      type: ociRegistry
    digest:
      hashAlgorithm: SHA-256
      normalisationAlgorithm: ociArtifactDigest/v1
      value: eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee
    extraIdentity:
      aa: b
      cc: dd
    labels: []
    name: res
    relation: external
    srcRefs: []
    type: ociImage
    version: 1.0.0
  sources: []
  version: 1.0.0
meta:
  schemaVersion: v2
nestedDigests: []
signatures: []
`
	norm_11_dup_delimiter   = `[{"component":[{"componentReferences":[]},{"name":"acme.org/dup-delim"},{"provider":"ACME"},{"resources":[[{"digest":[{"hashAlgorithm":"SHA-256"},{"normalisationAlgorithm":"ociArtifactDigest/v1"},{"value":"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"}]},{"extraIdentity":[{"aa":"b;cc=dd"}]},{"name":"res"},{"relation":"external"},{"type":"ociImage"},{"version":"1.0.0"}],[{"digest":[{"hashAlgorithm":"SHA-256"},{"normalisationAlgorithm":"ociArtifactDigest/v1"},{"value":"eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"}]},{"extraIdentity":[{"aa":"b"},{"cc":"dd"}]},{"name":"res"},{"relation":"external"},{"type":"ociImage"},{"version":"1.0.0"}]]},{"version":"1.0.0"}]},{"meta":[{"schemaVersion":"v2"}]}]`
	digest_11_dup_delimiter = `77ccb387feb90c986de9b14f2c034d09368ab487d37599d1028e81ccb89f649f`
)

const (
	input_12_dup_explicit_version = `
component:
  componentReferences: []
  creationTime: null
  labels: []
  name: acme.org/dup-ver
  provider: ACME
  repositoryContexts: []
  resources:
  - access:
      imageReference: registry.example/a@sha256:d1
      type: ociRegistry
    digest:
      hashAlgorithm: SHA-256
      normalisationAlgorithm: ociArtifactDigest/v1
      value: dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd
    extraIdentity: {}
    labels: []
    name: same
    relation: external
    srcRefs: []
    type: ociImage
    version: 1.0.0
  - access:
      imageReference: registry.example/a@sha256:d2
      type: ociRegistry
    digest:
      hashAlgorithm: SHA-256
      normalisationAlgorithm: ociArtifactDigest/v1
      value: eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee
    extraIdentity:
      version: 9.9.9
    labels: []
    name: same
    relation: external
    srcRefs: []
    type: ociImage
    version: 1.0.0
  sources: []
  version: 1.0.0
meta:
  schemaVersion: v2
nestedDigests: []
signatures: []
`
	norm_12_dup_explicit_version   = `[{"component":[{"componentReferences":[]},{"name":"acme.org/dup-ver"},{"provider":"ACME"},{"resources":[[{"digest":[{"hashAlgorithm":"SHA-256"},{"normalisationAlgorithm":"ociArtifactDigest/v1"},{"value":"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"}]},{"extraIdentity":null},{"name":"same"},{"relation":"external"},{"type":"ociImage"},{"version":"1.0.0"}],[{"digest":[{"hashAlgorithm":"SHA-256"},{"normalisationAlgorithm":"ociArtifactDigest/v1"},{"value":"eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"}]},{"extraIdentity":[{"version":"9.9.9"}]},{"name":"same"},{"relation":"external"},{"type":"ociImage"},{"version":"1.0.0"}]]},{"version":"1.0.0"}]},{"meta":[{"schemaVersion":"v2"}]}]`
	digest_12_dup_explicit_version = `2671e827b7ffb861f3d410e0e379e3a4f2c71b434488980157b87c4d2341926a`
)

const (
	input_14_label_list_value = `
component:
  componentReferences: []
  creationTime: null
  labels: []
  name: acme.org/label-list
  provider: ACME
  repositoryContexts: []
  resources:
  - access:
      imageReference: registry.example/img@sha256:cc
      type: ociRegistry
    digest:
      hashAlgorithm: SHA-256
      normalisationAlgorithm: ociArtifactDigest/v1
      value: cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
    extraIdentity: {}
    labels:
    - name: entries
      signing: true
      value:
      - a: x
        b: "y"
      - a: p
        b: q
      version: null
    name: img
    relation: external
    srcRefs: []
    type: ociImage
    version: 1.0.0
  sources: []
  version: 1.0.0
meta:
  schemaVersion: v2
nestedDigests: []
signatures: []
`
	norm_14_label_list_value   = `[{"component":[{"componentReferences":[]},{"name":"acme.org/label-list"},{"provider":"ACME"},{"resources":[[{"digest":[{"hashAlgorithm":"SHA-256"},{"normalisationAlgorithm":"ociArtifactDigest/v1"},{"value":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"}]},{"extraIdentity":null},{"labels":[[{"name":"entries"},{"signing":true},{"value":[{"a":"x","b":"y"},{"a":"p","b":"q"}]}]]},{"name":"img"},{"relation":"external"},{"type":"ociImage"},{"version":"1.0.0"}]]},{"version":"1.0.0"}]},{"meta":[{"schemaVersion":"v2"}]}]`
	digest_14_label_list_value = `11adb9f00d4ff1b88dbc8f45de40dcde09d759f603145d17e3d83c8fdb5d8470`
)
