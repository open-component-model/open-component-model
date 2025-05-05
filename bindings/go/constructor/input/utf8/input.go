package utf8

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/opencontainers/go-digest"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/compression"
	"ocm.software/open-component-model/bindings/go/constructor/input"
	"ocm.software/open-component-model/bindings/go/constructor/input/utf8/spec/v2alpha1"
	"ocm.software/open-component-model/bindings/go/constructor/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// MediaTypeTextPlain as per https://www.rfc-editor.org/rfc/rfc3676.html
	MediaTypeTextPlain = "text/plain"
	MediaTypeJSON      = "application/json"
	MediaTypeYAML      = "application/x-yaml"
)

var _ input.Method = &Method{}

type Method struct{ Scheme *runtime.Scheme }

func (i *Method) ProcessResource(_ context.Context, resource *spec.Resource) (data blob.ReadOnlyBlob, err error) {
	return i.process(resource)
}

func (i *Method) process(resource *spec.Resource) (blob.ReadOnlyBlob, error) {
	utf8 := v2alpha1.UTF8{}
	if err := i.Scheme.Convert(resource.Input, &utf8); err != nil {
		return nil, fmt.Errorf("error converting resource input spec: %w", err)
	}
	if err := validate(utf8); err != nil {
		return nil, fmt.Errorf("error validating resource input spec: %w", err)
	}

	var data blob.ReadOnlyBlob

	if utf8.HasText() {
		data = &PlainTextBlob{utf8.Text, utf8.MediaType}
	}

	if utf8.HasObject() {
		switch utf8.ObjectFormat {
		case v2alpha1.UTF8ObjectFormatJSON:
			fallthrough
		case v2alpha1.UTF8ObjectFormatYAML:
			data = &MarshalledBlob{
				Object:           utf8.Object.Data,
				UTF8ObjectFormat: utf8.ObjectFormat,
			}
		default:
			return nil, fmt.Errorf("unsupported object format %q", utf8.ObjectFormat)
		}
	}

	if utf8.Compress {
		data = compression.Compress(data)
	}

	cached := &CachedBlob{
		ReadOnlyBlob: data,
	}

	return cached, nil
}

func validate(t v2alpha1.UTF8) error {
	if !t.HasText() && !t.HasObject() {
		return fmt.Errorf("no text or object provided")
	}
	if t.HasText() && t.HasObject() {
		return fmt.Errorf("both text and object provided")
	}
	if !t.HasObject() && t.ObjectFormat != "" {
		return fmt.Errorf("object format provided without an object to apply the format to")
	}
	return nil
}

type CachedBlob struct {
	blob.ReadOnlyBlob
	data []byte
	mu   sync.RWMutex
}

func (c *CachedBlob) ReadCloser() (io.ReadCloser, error) {
	data, err := c.Data()
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (c *CachedBlob) Size() int64 {
	data, err := c.Data()
	if err != nil {
		return blob.SizeUnknown
	}
	return int64(len(data))
}

func (c *CachedBlob) Digest() (string, bool) {
	data, err := c.Data()
	if err != nil {
		return "", false
	}
	dig := digest.FromBytes(data)
	return dig.String(), true
}

func (c *CachedBlob) getData() []byte {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.data
}

func (c *CachedBlob) MediaType() (mediaType string, known bool) {
	if mediaTypeAware, ok := c.ReadOnlyBlob.(blob.MediaTypeAware); ok {
		return mediaTypeAware.MediaType()
	}
	return "", false
}

func (c *CachedBlob) setData(data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = data
}

func (c *CachedBlob) Data() ([]byte, error) {
	data := c.getData()
	if data != nil {
		return data, nil
	}

	dataStream, err := c.ReadOnlyBlob.ReadCloser()
	if err != nil {
		return nil, err
	}
	if data, err = io.ReadAll(dataStream); err != nil {
		return nil, err
	}
	c.setData(data)
	return data, nil
}

type MarshalledBlob struct {
	Object any
	v2alpha1.UTF8ObjectFormat
}

func (b *MarshalledBlob) MediaType() (mediaType string, known bool) {
	switch b.UTF8ObjectFormat {
	case v2alpha1.UTF8ObjectFormatJSON:
		return MediaTypeJSON, true
	case v2alpha1.UTF8ObjectFormatYAML:
		return MediaTypeYAML, true
	default:
		return "", false
	}
}

func (b *MarshalledBlob) ReadCloser() (io.ReadCloser, error) {
	reader, writer := io.Pipe()
	go marshal(b.Object, writer, b.UTF8ObjectFormat)
	return reader, nil
}

func marshal(object any, writer *io.PipeWriter, format v2alpha1.UTF8ObjectFormat) {
	var err error
	switch format {
	case v2alpha1.UTF8ObjectFormatJSON:
		err = json.NewEncoder(writer).Encode(object)
	case v2alpha1.UTF8ObjectFormatYAML:
		var data []byte
		if data, err = yaml.Marshal(object); err == nil {
			_, err = io.CopyN(writer, bytes.NewReader(data), int64(len(data)))
		}
	default:
		err = fmt.Errorf("unsupported object format %q", format)
	}
	writer.CloseWithError(err)
}

type PlainTextBlob struct {
	Text          string
	TextMediaType string
}

func (b *PlainTextBlob) ReadCloser() (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(b.Text)), nil
}

func (b *PlainTextBlob) MediaType() (mediaType string, known bool) {
	if b.TextMediaType != "" {
		return b.TextMediaType, true
	}
	return MediaTypeTextPlain, true
}

func (b *PlainTextBlob) Size() int64 {
	return int64(len(b.Text))
}

func (b *PlainTextBlob) Digest() (string, bool) {
	return digest.FromString(b.Text).String(), true
}
