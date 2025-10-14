package descriptor

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"

	"sigs.k8s.io/yaml"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// SingleFileEncodeDescriptor encodes a component descriptor into the requested media type.
func SingleFileEncodeDescriptor(scheme *runtime.Scheme, desc *descriptor.Descriptor, mediaType string) (buf *bytes.Buffer, err error) {
	v2desc, err := descriptor.ConvertToV2(scheme, desc)
	if err != nil {
		return nil, fmt.Errorf("unable to convert component descriptor into v2 representation: %w", err)
	}
	content, err := yaml.Marshal(v2desc)
	if err != nil {
		return nil, fmt.Errorf("unable to marshal descriptor as YAML: %w", err)
	}

	switch mediaType {
	case MediaTypeComponentDescriptorYAML, MediaTypeLegacyComponentDescriptorYAML:
		return bytes.NewBuffer(content), nil
	case MediaTypeComponentDescriptorJSON, MediaTypeLegacyComponentDescriptorJSON:
		converted, err := yaml.YAMLToJSONStrict(content)
		if err != nil {
			return nil, fmt.Errorf("unable to convert descriptor to JSON: %w", err)
		}
		return bytes.NewBuffer(converted), nil
	case MediaTypeLegacyComponentDescriptorTar,
		mediaTypeLegacy2ComponentDescriptorTar,
		mediaTypeLegacy3ComponentDescriptorTar:
		content, err := yaml.Marshal(v2desc)
		if err != nil {
			return nil, fmt.Errorf("unable to marshal descriptor for tar: %w", err)
		}

		var tarBuf bytes.Buffer
		tw := tar.NewWriter(&tarBuf)
		defer func() {
			err = errors.Join(err, tw.Close())
		}()

		if err := tw.WriteHeader(&tar.Header{
			Name: LegacyComponentDescriptorTarFileName,
			Mode: 0o644,
			Size: int64(len(content)),
		}); err != nil {
			return nil, fmt.Errorf("unable to write tar header: %w", err)
		}
		if _, err := tw.Write(content); err != nil {
			return nil, fmt.Errorf("unable to write tar content: %w", err)
		}
		return &tarBuf, nil

	default:
		return nil, fmt.Errorf("unsupported descriptor media type %s", mediaType)
	}
}
