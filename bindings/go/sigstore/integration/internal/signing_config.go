package internal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// signingConfig mirrors the cosign signing config protobuf JSON structure.
type signingConfig struct {
	MediaType     string         `json:"mediaType"`
	CAURLs        []serviceEntry `json:"caUrls"`
	RekorTlogURLs []serviceEntry `json:"rekorTlogUrls"`
	TSAURLs       []serviceEntry `json:"tsaUrls,omitempty"`
	RekorConfig   selectorConfig `json:"rekorTlogConfig"`
	TSAConfig     selectorConfig `json:"tsaConfig"`
}

type serviceEntry struct {
	URL             string   `json:"url"`
	MajorAPIVersion int      `json:"majorApiVersion"`
	ValidFor        validFor `json:"validFor"`
	Operator        string   `json:"operator"`
}

type validFor struct {
	Start time.Time `json:"start"`
}

type selectorConfig struct {
	Selector string `json:"selector,omitempty"`
}

// BuildSigningConfig writes a cosign signing_config.json that points at the
// given Fulcio, Rekor v2, and TSA URLs. The file is written to tmpDir.
func BuildSigningConfig(tmpDir, fulcioURL, rekorURL, tsaURL string) (string, error) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	cfg := signingConfig{
		MediaType: "application/vnd.dev.sigstore.signingconfig.v0.2+json",
		CAURLs: []serviceEntry{
			{
				URL:             fulcioURL,
				MajorAPIVersion: 1,
				ValidFor:        validFor{Start: start},
				Operator:        "integration-test",
			},
		},
		RekorTlogURLs: []serviceEntry{
			{
				URL:             rekorURL,
				MajorAPIVersion: 2,
				ValidFor:        validFor{Start: start},
				Operator:        "integration-test",
			},
		},
		RekorConfig: selectorConfig{Selector: "ANY"},
	}

	if tsaURL != "" {
		cfg.TSAURLs = []serviceEntry{
			{
				URL:             strings.TrimRight(tsaURL, "/") + "/api/v1/timestamp",
				MajorAPIVersion: 1,
				ValidFor:        validFor{Start: start},
				Operator:        "integration-test",
			},
		}
		cfg.TSAConfig = selectorConfig{Selector: "ANY"}
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}

	path := filepath.Join(tmpDir, "signing_config.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("write signing config: %w", err)
	}

	return path, nil
}
