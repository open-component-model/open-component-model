package manager

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// Sets up the HTTP client for the plugin or returns it, based on how I'm going to extract this.
func waitForPlugin(ctx context.Context, id, location string, typ ConnectionType) (*http.Client, error) {
	interval := 100 * time.Millisecond
	timer := time.NewTicker(interval)
	timeout := 5 * time.Second

	client, err := connect(ctx, id, location, typ)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to plugin %s: %w", id, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix/healthz", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to plugin %s: %w", id, err)
	}

	for {
		// This is the main work of the loop that we want to execute at least once
		// right away.
		if resp, err := client.Do(req); err == nil {
			_ = resp.Body.Close()

			return client, nil
		}

		select {
		case <-timer.C:
			// tick the loop and repeat the main loop body every set interval
		case <-time.After(timeout):
			return nil, fmt.Errorf("timed out waiting for plugin %s", id)
		case <-ctx.Done():
			return nil, fmt.Errorf("context was cancelled %s", id)
		}
	}
}

// connect will create a client that sets up connection based on the plugin's connection type.
// That is either a Unix socket or a TCP based connection. It does this by setting the `DialContext` using
// the right network location.
func connect(_ context.Context, id, location string, typ ConnectionType) (*http.Client, error) {
	var network string
	switch typ {
	case Socket:
		network = "unix"
	case TCP:
		network = "tcp"
	}

	dialer := net.Dialer{
		Timeout: 30 * time.Second,
	}

	// Create an HTTP client with the Unix socket connection
	client := &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        1000,
			MaxIdleConnsPerHost: 1000,
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				conn, err := dialer.DialContext(ctx, network, location)
				if err != nil {
					return nil, fmt.Errorf("failed to connect to plugin %s: %w", id, err)
				}

				return conn, nil
			},
		},
	}

	return client, nil
}

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

// call will use the plugin's constructed connection client to make a call to the specified
// endpoint. The result will be marshalled into the provided response if not nil.
func call(ctx context.Context, client *http.Client, endpoint, method string, opts ...CallOptionFn) (err error) {
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

	request, err := http.NewRequestWithContext(ctx, method, "http://unix/"+endpoint, body)
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
			return fmt.Errorf("plugin returned status code %d: %s", resp.StatusCode, data)
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
