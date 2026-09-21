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
// AccessDigest section that changes how the access-side digest processor
// records the resource digest.
//
// The digest recorded by the input method is always SHA-256; a source may
// verify the transferred bytes against a different algorithm (e.g. SHA-1 from a
// Maven repository) without changing the stored algorithm. The access-side
// digest processor, in contrast, MAY record any algorithm the source advertises
// when AccessDigest is set — see [AccessDigest] for why this is safe for an
// access (which references remote bytes) but not for an input (which embeds
// bytes as a local blob).
//
// This type lives here (not on the Wget/v1 input spec) because verification
// posture is a *deployment* concern, not a *descriptor* concern: the same
// component descriptor should behave identically wherever it is constructed,
// and *how* an operator wants to verify what a mirror serves is up to that
// operator. Producers who need construction to fail without verification
// simply refuse to construct without a wget-config; operators who trust their
// mirror configure `onMissing: compute`.
//
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type ChecksumPolicy struct {
	// Sources are the checksum strategies to try, in order. The first that
	// yields an expected checksum is used to verify the download (input side)
	// or to pin the resource digest without downloading (access side, when
	// AccessDigest is set).
	Sources []ChecksumSource `json:"sources,omitempty"`
	// OnMissing controls the behaviour when no source yields a checksum.
	// Defaults to "fail".
	// +ocm:jsonschema-gen:enum=fail,compute
	OnMissing OnMissingChecksum `json:"onMissing,omitempty"`
	// AccessDigest optionally opts the Wget/v1 access-side digest processor into
	// pinning the resource digest from a source-advertised checksum instead of
	// downloading the body and computing SHA-256. Nil preserves today's
	// download-and-hash behaviour. Ignored on the input side, which always
	// downloads and records SHA-256.
	AccessDigest *AccessDigest `json:"accessDigest,omitempty"`
}

// AccessDigest configures how the Wget/v1 access-side digest processor
// establishes the resource's digest. When set, the processor uses the
// source-advertised checksum (RFC 9530 Content-Digest, x-checksum-*, an
// externalUrl sidecar, …) as the pinned digest and does not download the body.
//
// This is safe for an access — but not for an input — for two reasons:
//
//   - An access references remote bytes that any consumer will re-fetch and
//     re-verify against the same source. Pinning what the source itself
//     advertises is a form of "I claim what you claim", which the downstream
//     verifier reproduces byte-for-byte on the next fetch.
//   - An input embeds the downloaded bytes as a local blob (LocalBlob/v1).
//     The resource identity is those bytes, so the digest MUST be computed
//     from them and MUST be SHA-256 with genericBlobDigest/v1 to match how
//     OCM stores every other local blob. Any transfer that promotes an access
//     to a local blob (see `--copy-resources`) therefore still streams the
//     bytes and computes SHA-256 regardless of this setting.
//
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type AccessDigest struct {
	// Algorithms lists the digest algorithms the processor will accept from the
	// source, strongest-preferred first. SHA-256 is preferred whenever the
	// source offers it; weaker algorithms are used only as a fallback. When
	// empty, the default set [sha256, sha512, sha1, md5] is used.
	Algorithms []string `json:"algorithms,omitempty"`
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
