//go:build wasip1

package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"

	"github.com/extism/go-pdk"
)

// ExtismRoundTripper implements http.RoundTripper using Extism's PDK HTTP functionality.
// This allows standard Go HTTP clients to work within the WASM sandbox by delegating
// network operations to the host via Extism's HTTP host functions.
type ExtismRoundTripper struct{}

// RoundTrip executes an HTTP request using Extism's PDK and returns the response.
func (e *ExtismRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// Create Extism HTTP request
	var method pdk.HTTPMethod
	switch req.Method {
	case "GET":
		method = pdk.MethodGet
	case "HEAD":
		method = pdk.MethodHead
	case "POST":
		method = pdk.MethodPost
	case "PUT":
		method = pdk.MethodPut
	case "PATCH":
		method = pdk.MethodPatch
	case "DELETE":
		method = pdk.MethodDelete
	case "CONNECT":
		method = pdk.MethodConnect
	case "OPTIONS":
		method = pdk.MethodOptions
	case "TRACE":
		method = pdk.MethodTrace
	}
	pdkReq := pdk.NewHTTPRequest(method, req.URL.String())

	// Copy headers from Go request to PDK request
	for key, values := range req.Header {
		for _, value := range values {
			pdkReq.SetHeader(key, value)
		}
	}

	// Copy request body if present
	if req.Body != nil {
		bodyBytes, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read request body: %w", err)
		}
		req.Body.Close()

		if len(bodyBytes) > 0 {
			pdkReq.SetBody(bodyBytes)
		}
	}

	// Send the request via Extism
	pdkResp := pdkReq.Send()

	// Check for errors
	if pdkResp.Status() == 0 {
		return nil, fmt.Errorf("HTTP request failed: status code 0")
	}

	// Convert PDK response to Go http.Response
	resp := &http.Response{
		StatusCode: int(pdkResp.Status()),
		Status:     http.StatusText(int(pdkResp.Status())),
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     make(http.Header),
		Request:    req,
	}

	// Copy response headers
	for key, value := range pdkResp.Headers() {
		resp.Header.Set(key, value)
	}

	// Set response body
	respBody := pdkResp.Body()
	resp.Body = io.NopCloser(bytes.NewReader(respBody))
	resp.ContentLength = int64(len(respBody))

	return resp, nil
}
