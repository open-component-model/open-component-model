// Package jev speaks the TypeSafe System One ("Jev") decisions API and turns a
// local repository into an OCM component-constructor scaffold by classifying it.
//
// Jev is a decisions model, NOT a chat/completions model. Two working transports:
//
//   - TypeSafe direct: POST https://api.typesafe.ai/v1/systemone
//   - OpenRouter:       POST https://openrouter.ai/api/alpha/decisions (model typesafe/jev-1.13)
//
// Both take a systemone-shaped body {"model","state","questions"} and return
// {"answers": {...}}. The decisions schema discriminator for a boolean question is
// "noul" (never "bool"); OpenRouter rejects "bool". This package always emits "noul".
package jev

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	ocmhttp "ocm.software/open-component-model/bindings/go/http"
)

// Question is a single Jev decision request. Type is one of "choice", "score",
// or "noul". Instructions is free-form guidance. Criteria is a map[string]string
// for choice/noul or a []string of levels for score (lowest -> highest).
type Question struct {
	Type         string `json:"type"`
	Instructions any    `json:"instructions"`
	Criteria     any    `json:"criteria,omitempty"`
}

// Answer is a single Jev decision result. Only the field matching the question
// type is populated: Choice for "choice", Score for "score", Noul for "noul".
type Answer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice,omitempty"`
	Score         float64            `json:"score,omitempty"`
	Noul          float64            `json:"noul,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	Confidence    float64            `json:"confidence,omitempty"`
}

// Client is a Jev decisions API client.
type Client struct {
	endpoint string
	model    string
	apiKey   string
	http     *http.Client
}

// NewClient constructs a Jev client for the given decisions endpoint and model,
// authenticating with a bearer apiKey. The httpClient is built by the caller
// from the OCM http.config.ocm.software configuration (retries, timeouts,
// per-host TLS) so the classifier honours the same HTTP policy as the rest of
// the CLI. When nil, the http binding's default client is used.
func NewClient(endpoint, model, apiKey string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = ocmhttp.New()
	}
	return &Client{
		endpoint: endpoint,
		model:    model,
		apiKey:   apiKey,
		http:     httpClient,
	}
}

type askRequest struct {
	Model     string              `json:"model"`
	State     any                 `json:"state"`
	Questions map[string]Question `json:"questions"`
}

type askResponse struct {
	Answers map[string]Answer `json:"answers"`
}

// Ask submits a batch of independent questions against the given state and
// returns the answers keyed by question name. Non-2xx responses return an error
// carrying the response body (the API returns actionable JSON errors, e.g. for a
// wrong endpoint or a bad discriminator).
func (c *Client) Ask(ctx context.Context, state any, questions map[string]Question) (map[string]Answer, error) {
	body, err := json.Marshal(askRequest{Model: c.model, State: state, Questions: questions})
	if err != nil {
		return nil, fmt.Errorf("marshaling jev request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("building jev request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling jev endpoint %q: %w", c.endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading jev response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("jev endpoint %q returned %s: %s", c.endpoint, resp.Status, string(respBody))
	}

	var decoded askResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("decoding jev response: %w (body: %s)", err, string(respBody))
	}
	if decoded.Answers == nil {
		return nil, fmt.Errorf("jev response contained no answers (body: %s)", string(respBody))
	}
	return decoded.Answers, nil
}
