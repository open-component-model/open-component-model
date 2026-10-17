package download

import (
	"net/http"

	"ocm.software/open-component-model/bindings/go/blob"
)

// representation is a response together with how its body maps to the content.
type representation struct {
	resp *http.Response
	// size is the content size, or [blob.SizeUnknown].
	size int64
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
