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
		if err := rejectV3Alpha1(&v2desc, descriptorYAML); err != nil {
			return nil, err
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
		if err := rejectV3Alpha1(&v2desc, descriptorYAML); err != nil {
			return nil, err
		}
		desc, err := descriptor.ConvertFromV2(&v2desc)
		if err != nil {
			return nil, fmt.Errorf("converting component descriptor: %w", err)
		}
		return desc, nil
	}
}

// ErrUnsupportedSchemaV3Alpha1 is returned when a descriptor is serialized in the
// deprecated ocm.software/v3alpha1 schema, which OCM v2 cannot read.
var ErrUnsupportedSchemaV3Alpha1 = errors.New("component descriptor uses the deprecated schema \"ocm.software/v3alpha1\", which is not supported; re-create it with the v2 schema (e.g. omit --scheme in OCM v1)")

const schemaV3Alpha1 = "ocm.software/v3alpha1"

// rejectV3Alpha1 detects descriptors in the v3alpha1 serialization (apiVersion or
// schemaVersion set to ocm.software/v3alpha1). They decode into a v2 descriptor without
// error but empty, and fail later with a misleading identity error. Every v2 descriptor
// sets meta.schemaVersion, so raw is only parsed again when that field is missing.
func rejectV3Alpha1(decoded *v2.Descriptor, raw []byte) error {
	switch decoded.Meta.Version {
	case schemaV3Alpha1:
		return ErrUnsupportedSchemaV3Alpha1
	case "":
	default:
		return nil
	}
	var probe struct {
		APIVersion string `json:"apiVersion"`
	}
	if err := yaml.Unmarshal(raw, &probe); err != nil {
		return nil // not our concern; the regular decoding reports syntax errors
	}
	if probe.APIVersion == schemaV3Alpha1 {
		return ErrUnsupportedSchemaV3Alpha1
	}
	return nil
}

const maxDescriptorSize = 1024 * 1024 * 1024 // 1 GiB

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
