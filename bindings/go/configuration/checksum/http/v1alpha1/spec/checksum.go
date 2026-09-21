package spec

// OnMissingChecksum controls policy behaviour when no source yields a checksum.
type OnMissingChecksum string

const (
	// OnMissingFail aborts with an error. Default when a ChecksumPolicy is set.
	OnMissingFail OnMissingChecksum = "fail"
	// OnMissingCompute records the digest computed from the stream, unverified.
	OnMissingCompute OnMissingChecksum = "compute"
)

// ChecksumSourceType selects a strategy for obtaining an expected checksum,
// modelled on Maven's expected-checksum strategies.
type ChecksumSourceType string

const (
	// ChecksumSourceHTTPHeader reads the checksum from the download response
	// headers (RFC 9530 Content-Digest and x-checksum-*).
	ChecksumSourceHTTPHeader ChecksumSourceType = "httpHeader"
	// ChecksumSourceExternalURL fetches the checksum from a sibling URL
	// (Maven's <url>.<ext> convention).
	ChecksumSourceExternalURL ChecksumSourceType = "externalUrl"
	// ChecksumSourceStream computes the digest from the stream without an
	// external expected checksum.
	ChecksumSourceStream ChecksumSourceType = "stream"
)

// ChecksumPolicy is an ordered list of checksum sources evaluated first-match
// wins, plus the behaviour when none yields a checksum, plus an optional
// algorithm preference used by the access-side digest processor when pinning
// from what the source advertises.
//
// Storage semantics differ per side. The input method always downloads and
// records SHA-256, regardless of which algorithm verified the transfer. The
// access-side digest processor records whichever algorithm the source
// advertises: this is safe because an access references remote bytes and any
// consumer re-fetches and re-verifies against the same source.
//
// Verification posture is a deployment concern, not a descriptor concern, so
// this type lives here rather than on the Wget/v1 input spec.
//
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type ChecksumPolicy struct {
	// Sources are the checksum strategies to try, in order.
	Sources []ChecksumSource `json:"sources,omitempty"`
	// OnMissing controls the behaviour when no source yields a checksum.
	// Defaults to "fail".
	// +ocm:jsonschema-gen:enum=fail,compute
	OnMissing OnMissingChecksum `json:"onMissing,omitempty"`
	// PreferredAlgorithms restricts and orders the algorithms the access-side
	// digest processor accepts when pinning. Strongest-preferred first; empty
	// means [sha256, sha512, sha1, md5]. Ignored on the input side.
	PreferredAlgorithms []string `json:"preferredAlgorithms,omitempty"`
}

// ChecksumSource configures a single checksum-retrieval strategy.
//
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type ChecksumSource struct {
	// Type selects the retrieval strategy.
	// +ocm:jsonschema-gen:enum=httpHeader,externalUrl,stream
	Type ChecksumSourceType `json:"type"`
	// Headers lists additional response header names to inspect for httpHeader
	// sources, beyond RFC 9530 and x-checksum-*. Algorithm is inferred from the
	// trailing token (e.g. "x-my-sha256").
	Headers []string `json:"headers,omitempty"`
	// URL is the absolute checksum URL for externalUrl sources; empty falls
	// back to <artifact URL>.<ext>. One URL per source: add more sources to
	// cover multiple algorithms or hosts.
	URL string `json:"url,omitempty"`
	// Algorithms restricts which algorithms this source considers (as file
	// extensions: sha256, sha512, sha1, md5), strongest-preferred first. Empty
	// means all supported.
	Algorithms []string `json:"algorithms,omitempty"`
}
