package handler

// CosignVersion is the version of the cosign binary that will be automatically
// downloaded when cosign is not found on PATH. This version is also used by all
// integration tests.
//
// renovate: datasource=github-releases depName=sigstore/cosign
const CosignVersion = "v3.0.6"
