// Package sbom assembles an "orchestrating" Software Bill of Materials for a
// whole OCM component version: a single hierarchical CycloneDX document that
// nests the discovered SBOM of every resource, and (recursively) of every
// referenced child component version.
//
// This package only assembles already-discovered SBOMs; discovery itself lives
// in the descriptor and oci bindings. Inputs may be SPDX or CycloneDX JSON and
// are normalized to CycloneDX before assembly.
package sbom

import (
	"bytes"
	"fmt"
	"io"

	cyclonedx "github.com/CycloneDX/cyclonedx-go"
	"github.com/protobom/protobom/pkg/formats"
	"github.com/protobom/protobom/pkg/reader"
	"github.com/protobom/protobom/pkg/writer"
)

// CycloneDXSpecVersion is the CycloneDX spec version emitted by this package.
const CycloneDXSpecVersion = cyclonedx.SpecVersion1_6

// NormalizeToCycloneDX reads an SBOM document (SPDX or CycloneDX JSON) and
// returns it as a CycloneDX 1.6 BOM.
//
// CycloneDX input is decoded directly. SPDX (and any other protobom-supported
// format) is routed through protobom's neutral document model and re-emitted as
// CycloneDX. The mediaType is a hint (e.g. oci.MediaTypeSPDXJSON); an empty or
// unknown value falls back to protobom's content sniffer.
func NormalizeToCycloneDX(r io.Reader, mediaType string) (*cyclonedx.BOM, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading SBOM failed: %w", err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("empty SBOM document")
	}

	if isCycloneDX(mediaType, data) {
		return decodeCycloneDX(data)
	}

	// Non-CycloneDX (e.g. SPDX): convert via protobom's neutral document.
	cdxData, err := convertToCycloneDX(data)
	if err != nil {
		return nil, err
	}
	return decodeCycloneDX(cdxData)
}

// convertToCycloneDX parses any protobom-supported SBOM and re-serializes it as
// CycloneDX 1.6 JSON.
func convertToCycloneDX(data []byte) ([]byte, error) {
	doc, err := reader.New().ParseStream(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("parsing SBOM for conversion failed: %w", err)
	}

	var buf bytes.Buffer
	if err := writer.New(writer.WithFormat(formats.CDX16JSON)).WriteStream(doc, &buf); err != nil {
		return nil, fmt.Errorf("re-encoding SBOM as CycloneDX failed: %w", err)
	}
	return buf.Bytes(), nil
}

func decodeCycloneDX(data []byte) (*cyclonedx.BOM, error) {
	bom := &cyclonedx.BOM{}
	if err := cyclonedx.NewBOMDecoder(bytes.NewReader(data), cyclonedx.BOMFileFormatJSON).Decode(bom); err != nil {
		return nil, fmt.Errorf("decoding CycloneDX SBOM failed: %w", err)
	}
	return bom, nil
}

// isCycloneDX decides whether data is already CycloneDX JSON, preferring the
// media type hint and falling back to a cheap content sniff for the CycloneDX
// discriminator field.
func isCycloneDX(mediaType string, data []byte) bool {
	switch mediaType {
	case "application/vnd.cyclonedx+json":
		return true
	case "application/spdx+json":
		return false
	}
	// Content sniff: CycloneDX documents carry "bomFormat":"CycloneDX";
	// SPDX documents carry "spdxVersion".
	return bytes.Contains(data, []byte(`"bomFormat"`)) && bytes.Contains(data, []byte(`"CycloneDX"`))
}
