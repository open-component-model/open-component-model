package tar

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"io"

	"sigs.k8s.io/yaml"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// SingleFileTARDecodeV2Descriptor decodes a component descriptor from a TAR archive.
func SingleFileTARDecodeV2Descriptor(raw io.Reader) (*descriptor.Descriptor, error) {
	const descriptorFileHeader = "component-descriptor.yaml"

	tarReader := tar.NewReader(raw)
	var buf bytes.Buffer
	found := false

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading tar header: %w", err)
		}

		switch header.Name {
		case descriptorFileHeader:
			if found {
				return nil, fmt.Errorf("multiple component-descriptor.yaml files found")
			}
			found = true
			if _, err := io.Copy(&buf, tarReader); err != nil {
				return nil, fmt.Errorf("reading component descriptor: %w", err)
			}
		default:
			if _, err := io.Copy(io.Discard, tarReader); err != nil {
				return nil, fmt.Errorf("skipping file %s: %w", header.Name, err)
			}
		}
	}

	if !found {
		return nil, fmt.Errorf("component-descriptor.yaml not found in archive")
	}

	var v2desc v2.Descriptor
	if err := yaml.Unmarshal(buf.Bytes(), &v2desc); err != nil {
		return nil, fmt.Errorf("unmarshaling component descriptor: %w", err)
	}
	desc, err := descriptor.ConvertFromV2(&v2desc)
	if err != nil {
		return nil, fmt.Errorf("converting component descriptor: %w", err)
	}

	return desc, nil
}

// SingleFileTAREncodeV2Descriptor encodes a component descriptor into a TAR archive.
func SingleFileTAREncodeV2Descriptor(scheme *runtime.Scheme, desc *descriptor.Descriptor) (encoding string, _ *bytes.Buffer, err error) {
	v2desc, err := descriptor.ConvertToV2(scheme, desc)
	if err != nil {
		return "", nil, fmt.Errorf("unable to convert component descriptor: %w", err)
	}

	descriptorEncoding := "+yaml"
	descriptorYAML, err := yaml.Marshal(v2desc)
	if err != nil {
		return "", nil, fmt.Errorf("unable to encode component descriptor: %w", err)
	}
	// prepare the descriptor
	descriptorEncoding += "+tar"
	var descriptorBuffer bytes.Buffer
	tarWriter := tar.NewWriter(&descriptorBuffer)
	defer func() {
		err = errors.Join(err, tarWriter.Close())
	}()

	if err := tarWriter.WriteHeader(&tar.Header{
		Name: "component-descriptor.yaml",
		Mode: 0644,
		Size: int64(len(descriptorYAML)),
	}); err != nil {
		return "", nil, fmt.Errorf("unable to write component descriptor header: %w", err)
	}
	if _, err := io.Copy(tarWriter, bytes.NewReader(descriptorYAML)); err != nil {
		return "", nil, err
	}
	return descriptorEncoding, &descriptorBuffer, nil
}
