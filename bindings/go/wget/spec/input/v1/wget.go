package v1

import (
	"errors"
	"fmt"
	"net/url"

	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	Type = "Wget"
)

// Wget describes an input sourced by downloading a resource from an HTTP/S URL
// during component construction. The downloaded content is stored as a local blob
// in the component version.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type Wget struct {
	// +ocm:jsonschema-gen:enum=wget/v1,Wget/v1
	// +ocm:jsonschema-gen:enum:deprecated=wget,Wget
	Type runtime.Type `json:"type"`

	// URL is the HTTP endpoint to download the resource from.
	URL string `json:"url"`

	// MediaType is the media type of the resource with optional format qualifiers.
	MediaType string `json:"mediaType,omitempty"`

	// Header contains HTTP headers to be sent with the request.
	Header map[string][]string `json:"header,omitempty"`

	// Verb is the HTTP method to use (GET, POST, etc.). Defaults to GET.
	Verb string `json:"verb,omitempty"`

	// Body is the HTTP body to send with the request.
	Body []byte `json:"body,omitempty"`

	// NoRedirect disables following HTTP redirects when set to true.
	NoRedirect bool `json:"noRedirect,omitempty"`

	// ChecksumPolicy optionally configures how an expected checksum for the
	// downloaded content is obtained and verified when the resource carries no
	// digest of its own. When unset, the digest is computed from the downloaded
	// stream without external verification. See [ChecksumPolicy].
	ChecksumPolicy *ChecksumPolicy `json:"checksumPolicy,omitempty"`
}

// OnMissingChecksum controls what happens when a [ChecksumPolicy] is configured
// but none of its sources yields an expected checksum.
type OnMissingChecksum string

const (
	// OnMissingFail aborts the input with an error when no source yields a checksum.
	// This is the default when a ChecksumPolicy is set.
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
	// ChecksumSourceStream computes the digest from the downloaded stream without
	// an external expected checksum.
	ChecksumSourceStream ChecksumSourceType = "stream"
)

// ChecksumPolicy is an ordered list of checksum sources evaluated first-match
// wins, plus the behaviour when none yields a checksum. The digest recorded on
// the resource is always SHA-256; a source may verify the transferred bytes
// against a different algorithm (e.g. SHA-1 from a Maven repository) without
// changing the stored algorithm.
//
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type ChecksumPolicy struct {
	// Sources are the checksum strategies to try, in order. The first that yields
	// an expected checksum is used to verify the download.
	Sources []ChecksumSource `json:"sources,omitempty"`
	// OnMissing controls the behaviour when no source yields a checksum. Defaults
	// to "fail".
	// +ocm:jsonschema-gen:enum=fail,compute
	OnMissing OnMissingChecksum `json:"onMissing,omitempty"`
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
	// Algorithms restricts which checksum algorithms this source considers, given
	// as file extensions (sha256, sha512, sha1, md5), strongest-preferred first.
	// When empty, all supported algorithms are considered.
	Algorithms []string `json:"algorithms,omitempty"`
}

func (t *Wget) String() string {
	return t.URL
}

// Validate verifies that the URL of the Wget input is set and uses a supported scheme.
func (t *Wget) Validate() error {
	if t.URL == "" {
		return errors.New("url is required")
	}
	parsed, err := url.Parse(t.URL)
	if err != nil {
		return fmt.Errorf("invalid url %q: %w", t.URL, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("url must use the http or https scheme, got %q", parsed.Scheme)
	}
	return nil
}
