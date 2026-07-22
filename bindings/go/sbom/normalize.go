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
	"strings"

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
// NormalizeToCycloneDX reads an SBOM document and returns it as a CycloneDX 1.6
// BOM. CycloneDX JSON and CycloneDX XML are decoded directly. SPDX (and any other
// protobom-supported format) is routed through protobom's neutral document model
// and re-emitted as CycloneDX. The mediaType is a hint; content is authoritative,
// so a mislabeled blob is still handled correctly.
func NormalizeToCycloneDX(r io.Reader, mediaType string) (*cyclonedx.BOM, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading SBOM failed: %w", err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("empty SBOM document")
	}

	switch detectFormat(mediaType, data) {
	case formatCycloneDXJSON:
		return decodeCycloneDX(data, cyclonedx.BOMFileFormatJSON)
	case formatCycloneDXXML:
		return decodeCycloneDX(data, cyclonedx.BOMFileFormatXML)
	default:
		// Non-CycloneDX (e.g. SPDX): convert via protobom's neutral document.
		cdxData, err := convertToCycloneDX(data)
		if err != nil {
			return nil, err
		}
		return decodeCycloneDX(cdxData, cyclonedx.BOMFileFormatJSON)
	}
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

func decodeCycloneDX(data []byte, format cyclonedx.BOMFileFormat) (*cyclonedx.BOM, error) {
	bom := &cyclonedx.BOM{}
	if err := cyclonedx.NewBOMDecoder(bytes.NewReader(data), format).Decode(bom); err != nil {
		return nil, fmt.Errorf("decoding CycloneDX SBOM failed: %w", err)
	}
	return bom, nil
}

type sbomFormat int

const (
	formatOther sbomFormat = iota
	formatCycloneDXJSON
	formatCycloneDXXML
)

// detectFormat classifies an SBOM document. Content is authoritative over the
// media-type hint, because SBOM producers frequently mislabel the blob; the media
// type is only consulted when the content is inconclusive.
func detectFormat(mediaType string, data []byte) sbomFormat {
	trimmed := bytes.TrimSpace(data)

	// CycloneDX XML: an XML document (root <bom>, optionally preceded by an XML
	// declaration) in the CycloneDX namespace.
	if bytes.HasPrefix(trimmed, []byte("<?xml")) || bytes.HasPrefix(trimmed, []byte("<bom")) {
		if bytes.Contains(trimmed, []byte("cyclonedx.org/schema/bom")) || bytes.Contains(trimmed, []byte("<bom")) {
			return formatCycloneDXXML
		}
		return formatOther
	}

	// CycloneDX JSON carries "bomFormat":"CycloneDX".
	if bytes.Contains(trimmed, []byte(`"bomFormat"`)) && bytes.Contains(trimmed, []byte(`"CycloneDX"`)) {
		return formatCycloneDXJSON
	}
	// SPDX JSON carries "spdxVersion"; route through protobom.
	if bytes.Contains(trimmed, []byte(`"spdxVersion"`)) {
		return formatOther
	}

	// Content inconclusive: fall back to the media-type hint.
	switch {
	case strings.Contains(mediaType, "cyclonedx") && strings.Contains(mediaType, "xml"):
		return formatCycloneDXXML
	case strings.Contains(mediaType, "cyclonedx") && strings.Contains(mediaType, "json"):
		return formatCycloneDXJSON
	default:
		return formatOther
	}
}
