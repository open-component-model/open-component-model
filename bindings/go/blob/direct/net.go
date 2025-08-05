package direct

import (
	"net/http"
)

// NewFromHTTPResponse creates a Blob from an http.Response, using the response body as the source.
// It sets the media type from the "Content-Type" header and the size from the Content
// Length header. Additional DirectBlobOption values can be provided to further configure the Blob.
func NewFromHTTPResponse(resp *http.Response, opts ...DirectBlobOption) *Blob {
	return New(resp.Body,
		append([]DirectBlobOption{
			WithMediaType(resp.Header.Get("Content-Type")),
			WithSize(resp.ContentLength),
		}, opts...)...)
}
