package download

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"ocm.software/open-component-model/bindings/go/blob"
)

// representation is a response together with how its body maps to the content.
type representation struct {
	resp *http.Response
	// decoded reports that the body is gzip and has to be decoded into the content.
	decoded bool
	// size is the content size, or [blob.SizeUnknown].
	size int64
}

// resolve decides how resp's body maps to the content. Without compressTransfer
// the body is the content. Otherwise the content is what a request accepting no
// compression yields (see docs/adr/0034_wget_transfer_compression.md): a
// Content-Encoding the server keeps for an identity request is part of the
// content, a negotiated gzip encoding is decoded.
func (f *fetcher) resolve(r *http.Request, resp *http.Response) (representation, error) {
	coding := contentCoding(resp.Header)
	if !f.compressTransfer || coding == "" {
		return representation{resp: resp, size: bodySize(resp)}, nil
	}

	probe := f.probeIdentity()
	if probe.ok && probe.coding != "" {
		slog.DebugContext(r.Context(), "wget: content encoding is part of the content, keeping it",
			"url", f.safeURL, "contentEncoding", coding)
		return representation{resp: resp, size: bodySize(resp)}, nil
	}
	if coding != "gzip" && coding != "x-gzip" {
		return representation{}, fmt.Errorf("response from %s uses unsupported content encoding %q", f.safeURL, coding)
	}
	size := blob.SizeUnknown
	if probe.ok {
		size = probe.size
	}
	slog.DebugContext(r.Context(), "wget: decoding negotiated gzip transfer compression",
		"url", f.safeURL, "probed", probe.ok, "size", size)
	return representation{resp: resp, decoded: true, size: size}, nil
}

// bodySize returns the number of bytes resp's body yields, or [blob.SizeUnknown].
func bodySize(resp *http.Response) int64 {
	switch {
	case resp.Request != nil && resp.Request.Method == http.MethodHead:
		return 0
	case resp.ContentLength >= 0 && !resp.Uncompressed:
		return resp.ContentLength
	default:
		return blob.SizeUnknown
	}
}

// contentCoding returns the normalised Content-Encoding of header; identity is
// reported as none.
func contentCoding(header http.Header) string {
	coding := strings.ToLower(strings.TrimSpace(header.Get("Content-Encoding")))
	if coding == "identity" {
		return ""
	}
	return coding
}

// identityProbe is what an identity request reports about the content.
type identityProbe struct {
	// ok reports that the probe got a successful response announcing a size or
	// a content coding.
	ok bool
	// coding is the Content-Encoding the server keeps for an identity request.
	coding string
	// size is the size of the identity representation, or [blob.SizeUnknown].
	size int64
}

// probeIdentity repeats the request accepting no compression and asking for the
// first byte only, so the response headers describe the identity representation
// without transferring it. The body is never read. Only GET is probed: repeating
// other methods may have side effects.
func (f *fetcher) probeIdentity() identityProbe {
	if f.request.Method != http.MethodGet {
		return identityProbe{}
	}
	r, err := f.clone()
	if err != nil {
		return identityProbe{}
	}
	r.Header.Set("Accept-Encoding", "identity")
	r.Header.Set("Range", "bytes=0-0")

	resp, err := f.client.Do(r)
	if err != nil {
		slog.DebugContext(r.Context(), "wget: identity probe failed", "url", f.safeURL, "err", err)
		return identityProbe{}
	}
	_ = resp.Body.Close()

	probe := identityProbe{coding: contentCoding(resp.Header), size: blob.SizeUnknown}
	switch resp.StatusCode {
	case http.StatusPartialContent:
		// Content-Range: bytes 0-0/<total>; the total is "*" when unknown.
		_, total, found := strings.Cut(resp.Header.Get("Content-Range"), "/")
		if n, err := strconv.ParseInt(strings.TrimSpace(total), 10, 64); found && err == nil && n >= 0 {
			probe.size = n
		}
	case http.StatusOK:
		// The server ignored the range and announced the whole representation.
		probe.size = resp.ContentLength
	default:
		return identityProbe{}
	}
	probe.ok = probe.size >= 0 || probe.coding != ""
	return probe
}
