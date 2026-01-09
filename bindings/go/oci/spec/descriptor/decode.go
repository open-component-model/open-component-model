package descriptor

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"sigs.k8s.io/yaml"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
)

// UnmarshalFunc is a function that unmarshals a descriptor from bytes.
type UnmarshalFunc func(mediaType string, bytes []byte, obj interface{}) error

// SingleFileDecodeDescriptor decodes a component descriptor from a TAR archive.
func SingleFileDecodeDescriptor(raw io.Reader, mediaType string, unmarshal UnmarshalFunc) (*descriptor.Descriptor, error) {
	switch mediaType {
	case MediaTypeLegacyComponentDescriptorTar,
		mediaTypeLegacy2ComponentDescriptorTar,
		mediaTypeLegacy3ComponentDescriptorTar:
		descriptorStream, err := descriptorFileFromTar(raw)
		if err != nil {
			return nil, fmt.Errorf("unable to get component descriptor stream from tar: %w", err)
		}
		descriptorYAML, err := io.ReadAll(descriptorStream)
		if err != nil {
			return nil, fmt.Errorf("unable to read component descriptor stream from tar: %w", err)
		}
		var v2desc v2.Descriptor
		if err := unmarshal(MediaTypeLegacyComponentDescriptorYAML, descriptorYAML, &v2desc); err != nil {
			return nil, fmt.Errorf("unmarshaling component descriptor: %w", err)
		}
		desc, err := descriptor.ConvertFromV2(&v2desc)
		if err != nil {
			return nil, fmt.Errorf("converting component descriptor: %w", err)
		}
		return desc, nil
	default:
		descriptorYAML, err := io.ReadAll(raw)
		if err != nil {
			return nil, fmt.Errorf("unable to read component descriptor stream from descriptor with format %q: %w", mediaType, err)
		}
		var v2desc v2.Descriptor
		if err := unmarshal(mediaType, descriptorYAML, &v2desc); err != nil {
			return nil, fmt.Errorf("unmarshaling component descriptor: %w", err)
		}
		desc, err := descriptor.ConvertFromV2(&v2desc)
		if err != nil {
			return nil, fmt.Errorf("converting component descriptor: %w", err)
		}
		return desc, nil
	}
}

const maxDescriptorSize = 1 ^ 1024*1024*1024 // 1 GB

// descriptorFileFromTar reads the component descriptor from a tar.
// The component is expected to be inside the tar in a file called LegacyComponentDescriptorTarFileName.
func descriptorFileFromTar(r io.Reader) (io.Reader, error) {
	tr := tar.NewReader(r)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, errors.New("no component descriptor found available in tar")
		}
		if err != nil {
			return nil, fmt.Errorf("unable to read tar: %w", err)
		}

		if strings.TrimLeft(header.Name, "/") == LegacyComponentDescriptorTarFileName {
			if header.Size > maxDescriptorSize {
				return nil, fmt.Errorf("component descriptor is too large: %d bytes", maxDescriptorSize)
			}
			return io.LimitReader(tr, header.Size), nil
		}

		slog.Debug("skipping file in descriptor tar", slog.String("file", header.Name))
		if _, err := io.CopyN(io.Discard, tr, header.Size); err != nil {
			return nil, fmt.Errorf("failed skipping file %s: %w", header.Name, err)
		}
	}
}

// DefaultDescriptorUnmarshalFunc is the default descriptor unmarshal function used by the repository
// to unmarshal component descriptors from OCI stores.
//
// This function supports JSON and YAML encoded component descriptors and will use the well known
// v2 validation functions to ensure the integrity of the descriptor data.
func DefaultDescriptorUnmarshalFunc(mediaType string, bytes []byte, obj interface{}) error {
	var err error
	switch mediaType {
	case MediaTypeComponentDescriptorJSON, MediaTypeLegacyComponentDescriptorJSON:
		err = v2.ValidateRawJSON(bytes)
	case MediaTypeComponentDescriptorYAML, MediaTypeLegacyComponentDescriptorYAML:
		err = v2.ValidateRawYAML(bytes)
	default:
		return fmt.Errorf("unsupported media type %q", mediaType)
	}
	if err != nil {
		slog.Warn("failed to validate descriptor", slog.String("mediaType", mediaType), slog.String("error", err.Error()))
	}
	return yaml.Unmarshal(bytes, obj)
}
