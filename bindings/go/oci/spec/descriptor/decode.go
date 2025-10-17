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

// SingleFileDecodeDescriptor decodes a component descriptor from a TAR archive.
func SingleFileDecodeDescriptor(raw io.Reader, mediaType string) (*descriptor.Descriptor, error) {
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
		if err := yaml.UnmarshalStrict(descriptorYAML, &v2desc); err != nil {
			return nil, fmt.Errorf("unmarshaling component descriptor: %w", err)
		}
		desc, err := descriptor.ConvertFromV2(&v2desc)
		if err != nil {
			return nil, fmt.Errorf("converting component descriptor: %w", err)
		}
		return desc, nil
	case MediaTypeComponentDescriptorJSON, MediaTypeLegacyComponentDescriptorJSON,
		MediaTypeComponentDescriptorYAML, MediaTypeLegacyComponentDescriptorYAML:
		descriptorYAML, err := io.ReadAll(raw)
		if err != nil {
			return nil, fmt.Errorf("unable to read component descriptor stream from descriptor with format %q: %w", mediaType, err)
		}
		var v2desc v2.Descriptor
		if err := yaml.UnmarshalStrict(descriptorYAML, &v2desc); err != nil {
			return nil, fmt.Errorf("unmarshaling component descriptor: %w", err)
		}
		desc, err := descriptor.ConvertFromV2(&v2desc)
		if err != nil {
			return nil, fmt.Errorf("converting component descriptor: %w", err)
		}
		return desc, nil
	default:
		return nil, fmt.Errorf("unsupported descriptor media type %s", mediaType)
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
