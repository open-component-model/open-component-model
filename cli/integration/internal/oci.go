package internal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/oci/tar"
)

func CreateSingleLayerOCIImageLayoutTar(t *testing.T, data []byte, ref ...string) *bytes.Buffer {
	t.Helper()
	r := require.New(t)
	var buf bytes.Buffer
	w := tar.NewOCILayoutWriter(&buf)

	desc := ociImageSpecV1.Descriptor{}
	desc.Digest = digest.FromBytes(data)
	desc.Size = int64(len(data))
	desc.MediaType = ociImageSpecV1.MediaTypeImageLayer

	r.NoError(w.Push(t.Context(), desc, bytes.NewReader(data)))

	configRaw, err := json.Marshal(map[string]string{})
	r.NoError(err)
	configDesc := ociImageSpecV1.Descriptor{
		Digest:    digest.FromBytes(configRaw),
		Size:      int64(len(configRaw)),
		MediaType: "application/json",
	}
	r.NoError(w.Push(t.Context(), configDesc, bytes.NewReader(configRaw)))

	manifest := ociImageSpecV1.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Config:    configDesc,
		Layers: []ociImageSpecV1.Descriptor{
			desc,
		},
	}
	manifestRaw, err := json.Marshal(manifest)
	r.NoError(err)

	manifestDesc := ociImageSpecV1.Descriptor{
		Digest:    digest.FromBytes(manifestRaw),
		Size:      int64(len(manifestRaw)),
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
	}
	r.NoError(w.Push(t.Context(), manifestDesc, bytes.NewReader(manifestRaw)))

	for _, ref := range ref {
		r.NoError(w.Tag(t.Context(), manifestDesc, ref))
	}

	r.NoError(w.Close())

	return &buf
}

type OCIRegistry struct {
	User            string
	Password        string
	RegistryAddress string
	Host            string
	Port            string
}

func CreateOCIRegistry(t *testing.T) (*OCIRegistry, error) {
	user := "ocm"
	password := GenerateRandomPassword(t, 20)
	htpasswd := GenerateHtpasswd(t, user, password)

	containerName := fmt.Sprintf("%s-repository-%d", strings.ToLower(t.Name()), time.Now().UnixNano())
	registryAddress := StartDockerContainerRegistry(t, containerName, htpasswd)
	host, port, err := net.SplitHostPort(registryAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to parse registry address: %w", err)
	}

	return &OCIRegistry{
		User:            user,
		Password:        password,
		RegistryAddress: registryAddress,
		Host:            host,
		Port:            port,
	}, nil
}

func (r *OCIRegistry) Reference(ref string) string {
	return fmt.Sprintf("%s/%s", r.RegistryAddress, ref)
}
