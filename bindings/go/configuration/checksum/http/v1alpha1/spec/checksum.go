package spec

// OnMissingChecksum controls what happens when a [ChecksumPolicy] is configured
// but none of its sources yields an expected checksum.
type OnMissingChecksum string

const (
	// OnMissingFail aborts the input or digest processor with an error when no
	// source yields a checksum. Default when a ChecksumPolicy is set.
	OnMissingFail OnMissingChecksum = "fail"
	// OnMissingCompute falls back to computing the digest from the stream without
	// external verification when no source yields a checksum.
	OnMissingCompute OnMissingChecksum = "compute"
)

// ChecksumSourceType selects a strategy for obtaining an expected checksum,
// modelled on Maven's expected-checksum strategies.
type ChecksumSourceType string

const (
	// ChecksumSourceHTTPHeader reads the checksum from the download response
	// headers ("Remote Included"): the RFC 9530 Content-Digest field and the
	// non-standard x-checksum-* family.
	ChecksumSourceHTTPHeader ChecksumSourceType = "httpHeader"
	// ChecksumSourceExternalURL fetches the checksum from a sibling URL
	// ("Remote External"), e.g. <url>.sha256.
	ChecksumSourceExternalURL ChecksumSourceType = "externalUrl"
	// ChecksumSourceStream computes the digest from the downloaded stream
	// without an external expected checksum.
	ChecksumSourceStream ChecksumSourceType = "stream"
)

// ChecksumPolicy is an ordered list of checksum sources evaluated first-match
// wins, plus the behaviour when none yields a checksum, plus an optional
// algorithm preference list that the access-side digest processor consults
// when pinning the resource digest from what the source advertises.
//
// The digest recorded by the input method is always SHA-256; a source may
// verify the transferred bytes against a different algorithm (e.g. SHA-1 from
// a Maven repository) without changing the stored algorithm. The access-side
// digest processor, in contrast, records whichever algorithm the source
// advertises for the pin — see below for why this is safe for an access
// (which references remote bytes) but not for an input (which embeds bytes as
// a local blob).
//
// This type lives here (not on the Wget/v1 input spec) because verification
// posture is a *deployment* concern, not a *descriptor* concern: the same
// component descriptor should behave identically wherever it is constructed,
// and *how* an operator wants to verify what a mirror serves is up to that
// operator. Producers who need construction to fail without verification
// simply refuse to construct without a checksum-http config; operators who
// trust their mirror configure `onMissing: compute`.
//
// Access-side fast path — no body download.
//
// Whenever a policy applies on the access side, the digest processor pins the
// resource digest from what the source advertises (a HEAD to the artifact
// URL, plus a sidecar GET per externalUrl source). It does not fetch the
// body. This is safe for two reasons:
//
//   - An access references remote bytes that any consumer will re-fetch and
//     re-verify against the same source. Pinning what the source itself
//     advertises is a form of "I claim what you claim", which the downstream
//     verifier reproduces byte-for-byte on the next fetch.
//   - An input embeds the downloaded bytes as a local blob (LocalBlob/v1).
//     The resource identity is those bytes, so the input path MUST compute
//     SHA-256 from the stream. Any transfer that promotes an access to a
//     local blob (see `--copy-resources`) therefore still streams the bytes
//     and computes SHA-256, regardless of what this policy prefers.
//
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type ChecksumPolicy struct {
	// Sources are the checksum strategies to try, in order. The first that
	// yields an expected checksum is used to verify the download (input side)
	// or to pin the resource digest without downloading (access side).
	Sources []ChecksumSource `json:"sources,omitempty"`
	// OnMissing controls the behaviour when no source yields a checksum.
	// Defaults to "fail".
	// +ocm:jsonschema-gen:enum=fail,compute
	OnMissing OnMissingChecksum `json:"onMissing,omitempty"`
	// PreferredAlgorithms restricts and orders the digest algorithms the
	// access-side digest processor accepts when pinning from what the source
	// advertises. Strongest-preferred first: an advertised digest is used
	// only when its algorithm appears in this list, and among competing
	// offers the earliest match wins.
	//
	// Empty means "any supported algorithm, [sha256, sha512, sha1, md5]".
	// Ignored on the input side, which always downloads and records SHA-256.
	PreferredAlgorithms []string `json:"preferredAlgorithms,omitempty"`
}

// ChecksumSource configures a single checksum-retrieval strategy within a
// [ChecksumPolicy].
//
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type ChecksumSource struct {
	// Type selects the retrieval strategy.
	// +ocm:jsonschema-gen:enum=httpHeader,externalUrl,stream
	Type ChecksumSourceType `json:"type"`
	// Headers lists additional response header names to inspect for httpHeader
	// sources, beyond the standard RFC 9530 and x-checksum-* headers. The
	// algorithm is inferred from a trailing token (e.g. "x-my-sha256").
	Headers []string `json:"headers,omitempty"`
	// URL is the absolute URL of the checksum resource for externalUrl sources.
	// Empty falls back to `<artifact URL>.<ext>` (Maven's convention). One URL
	// per source: to cover multiple algorithms or hosts, add multiple sources.
	URL string `json:"url,omitempty"`
	// Algorithms restricts which checksum algorithms this source considers,
	// given as file extensions (sha256, sha512, sha1, md5), strongest-preferred
	// first. When empty, all supported algorithms are considered.
	Algorithms []string `json:"algorithms,omitempty"`
}
