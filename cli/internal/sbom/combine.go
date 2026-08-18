package sbom

import (
	"bytes"
	"crypto"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/protobom/protobom/pkg/formats"
	"github.com/protobom/protobom/pkg/reader"
	protosbom "github.com/protobom/protobom/pkg/sbom"
	"github.com/protobom/protobom/pkg/writer"
	"google.golang.org/protobuf/types/known/timestamppb"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
)

const (
	FormatSPDX      = "spdx"
	FormatCycloneDX = "cyclonedx"
)

// Formats lists the accepted --sbom-format values, in flag order.
var Formats = []string{FormatSPDX, FormatCycloneDX}

// serializeAs maps a format name onto the concrete protobom format
var serializeAs = map[string]formats.Format{
	FormatSPDX:      formats.SPDX23JSON,
	FormatCycloneDX: formats.CDX16JSON,
}

// Combine folds every discovered sbom into a single SBOM rooted at the resource
// they describe. now is used as the creation timestamp of the combined document.
//
// This document is a new document. The original digest information is discarded.
func Combine(sboms []repository.SBOM, res *descriptor.Resource, now time.Time) (*protosbom.Document, error) {
	if len(sboms) == 0 {
		return nil, fmt.Errorf("no sbom documents to combine")
	}

	parser := reader.New()

	combined := protosbom.NewNodeList()
	for _, discovered := range sboms {
		parsed, err := parser.ParseStream(bytes.NewReader(discovered.Data))
		if err != nil {
			// repository.SBOM stringifies to its name, never its contents.
			return nil, fmt.Errorf("parsing discovered sbom %q failed: %w", discovered, err)
		}
		combined = combined.Union(parsed.NodeList)
	}

	root, err := rootNode(res)
	if err != nil {
		return nil, fmt.Errorf("failed to create root node: %w", err)
	}
	sources := combined.RootElements
	combined.AddNode(root)
	for _, source := range sources {
		combined.AddEdge(&protosbom.Edge{
			Type: protosbom.Edge_contains,
			From: root.Id,
			To:   []string{source},
		})
	}

	// root id is always required.
	combined.RootElements = []string{root.Id}

	document := protosbom.NewDocument()
	document.NodeList = combined
	document.Metadata = constructMetadata(res, now)

	return document, nil
}

// Write serialises the combined document.
func Write(document *protosbom.Document, format string, out io.Writer) error {
	target, ok := serializeAs[format]
	if !ok {
		return fmt.Errorf("unsupported sbom format %q, must be one of %v", format, Formats)
	}
	if err := writer.New(writer.WithFormat(target)).WriteStream(document, out); err != nil {
		return fmt.Errorf("serializing combined sbom as %s failed: %w", format, err)
	}
	return nil
}

// rootID names the root after the resource identity.
//
// SPDX regex for ids is such: "SPDXRef-[a-zA-Z0-9.-]+". So this creates a valid root id from
// an identity.
func rootID(res *descriptor.Resource) string {
	identity := res.ToIdentity().String()

	var b strings.Builder
	b.Grow(len("ocm-resource-") + len(identity))
	b.WriteString("ocm-resource-")
	for _, r := range identity {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return b.String()
}

// rootNode is the root node of our combined document.
func rootNode(res *descriptor.Resource) (*protosbom.Node, error) {
	node := &protosbom.Node{
		Id:      rootID(res),
		Type:    protosbom.Node_PACKAGE,
		Name:    res.Name,
		Version: res.Version,
	}
	if res.Digest != nil && res.Digest.Value != "" {
		var algo int32
		switch res.Digest.HashAlgorithm {
		case crypto.SHA256.String():
			algo = int32(protosbom.HashAlgorithm_SHA256)
		case crypto.SHA512.String():
			algo = int32(protosbom.HashAlgorithm_SHA512)
		default:
			return nil, fmt.Errorf("unsupported digest algorithm: %s", res.Digest.HashAlgorithm)
		}
		node.Hashes = map[int32]string{
			algo: res.Digest.Value,
		}
	}
	return node, nil
}

// constructMetadata sets the combined document's own metadata.
func constructMetadata(res *descriptor.Resource, now time.Time) *protosbom.Metadata {
	return &protosbom.Metadata{
		Id:      rootID(res),
		Name:    res.Name,
		Version: res.Version,
		Date:    timestamppb.New(now),
		Tools:   []*protosbom.Tool{{Name: "ocm", Vendor: "Open Component Model"}},
		Comment: "Combined from the SBOMs discovered for this resource. Derived document: " +
			"it does not match the bytes of any published sbom.",
	}
}
