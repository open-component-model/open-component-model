package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
)

// CallOptions contains options for calling a plugin endpoint.
type CallOptions struct {
	Payload     any
	Result      any
	Headers     []KV
	QueryParams []KV
}

// CallOptionFn defines a function that sets parameters for the Call method.
type CallOptionFn func(opt *CallOptions)

// WithPayload sets up payload to send to the callee
func WithPayload(payload any) CallOptionFn {
	return func(opt *CallOptions) {
		opt.Payload = payload
	}
}

// WithResult sets up a result that the call will marshal into.
func WithResult(result any) CallOptionFn {
	return func(opt *CallOptions) {
		opt.Result = result
	}
}

// WithHeaders sets headers for the call.
func WithHeaders(headers []KV) CallOptionFn {
	return func(opt *CallOptions) {
		opt.Headers = headers
	}
}

// WithHeader sets a specific header for the call.
func WithHeader(header KV) CallOptionFn {
	return func(opt *CallOptions) {
		opt.Headers = append(opt.Headers, header)
	}
}

// WithQueryParams sets url parameters for the call.
func WithQueryParams(queryParams []KV) CallOptionFn {
	return func(opt *CallOptions) {
		opt.QueryParams = queryParams
	}
}

// Call will use the plugin's constructed connection client to make a call to the specified
// endpoint. The result will be marshalled into the provided response if not nil.
func Call(ctx context.Context, client *http.Client, locationType types.ConnectionType, location, endpoint, method string, opts ...CallOptionFn) (err error) {
	options := &CallOptions{}
	for _, opt := range opts {
		opt(options)
	}

	var body io.Reader
	if options.Payload != nil {
		content, err := json.Marshal(options.Payload)
		if err != nil {
			return fmt.Errorf("failed to marshal payload: %w", err)
		}

		body = bytes.NewReader(content)
	}

	base := "http://unix"
	if locationType == types.TCP {
		base = location
	}

	// always ensure that we aren't starting with a `/`.
	endpoint = strings.TrimPrefix(endpoint, "/")
	request, err := http.NewRequestWithContext(ctx, method, base+"/"+endpoint, body)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	if len(options.QueryParams) > 0 {
		query := request.URL.Query()
		for _, kv := range options.QueryParams {
			query.Add(kv.Key, kv.Value)
		}

		request.URL.RawQuery = query.Encode()
	}

	for _, v := range options.Headers {
		request.Header.Add(v.Key, v.Value)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")

	resp, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("failed to send request to plugin: %w", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			err = errors.Join(err, cerr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		data, err := io.ReadAll(resp.Body)
		if err == nil && len(data) > 0 {
			return fmt.Errorf("plugin returned status code %d: additional information: %s", resp.StatusCode, data)
		}

		return fmt.Errorf("plugin returned status code: %d (no details were given)", resp.StatusCode)
	}

	if options.Result == nil {
		// Discard the body content otherwise some gibberish might remain in it
		// that messes up further connections.
		_, err = io.Copy(io.Discard, resp.Body)
		if err != nil {
			return fmt.Errorf("failed to read response body: %w", err)
		}

		return nil
	}

	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&options.Result); err != nil {
		return fmt.Errorf("failed to decode response from plugin: %w", err)
	}

	return nil
}
