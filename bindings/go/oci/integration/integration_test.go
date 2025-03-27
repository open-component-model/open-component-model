package integration_test

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/registry"
	"golang.org/x/crypto/bcrypt"
	"oras.land/oras-go/v2/errdef"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"

	"ocm.software/open-component-model/bindings/go/oci"
)

const (
	distributionRegistryImage = "registry:2.8.3"
	testUsername              = "ocm"
	passwordLength            = 20
	charset                   = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*()-_=+[]{}<>?"
	userAgent                 = "ocm.software"
)

func Test_Integration_OCIRepository(t *testing.T) {
	ctx := t.Context()
	require := require.New(t)

	t.Logf("Starting OCI integration test")

	// Setup credentials and htpasswd
	password := generateRandomPassword(t, passwordLength)
	htpasswd := generateHtpasswd(t, testUsername, password)

	// Start containerized registry
	t.Logf("Launching test registry (%s)...", distributionRegistryImage)
	registryContainer, err := registry.Run(ctx, distributionRegistryImage, registry.WithHtpasswd(htpasswd))
	require.NoError(err)
	t.Cleanup(func() {
		require.NoError(testcontainers.TerminateContainer(registryContainer))
	})
	t.Logf("Test registry started")

	registryAddress, err := registryContainer.HostAddress(ctx)
	require.NoError(err)

	reference := func(image string) string {
		return fmt.Sprintf("%s/%s", registryAddress, image)
	}

	client := createAuthClient(registryAddress, testUsername, password)

	t.Run("basic connectivity and resolution failure", func(t *testing.T) {
		testResolverConnectivity(t, registryAddress, reference("target:latest"), client)
	})
}

func testResolverConnectivity(t *testing.T, registryAddr, imageRef string, client *auth.Client) {
	ctx := t.Context()
	require := require.New(t)

	resolver := oci.NewURLPathResolver(registryAddr)
	resolver.SetClient(client)
	resolver.PlainHTTP = true

	store, err := resolver.StoreForReference(ctx, imageRef)
	require.NoError(err)

	_, err = store.Resolve(ctx, imageRef)
	require.ErrorIs(err, errdef.ErrNotFound)
	require.ErrorContains(err, fmt.Sprintf("%s: not found", imageRef))
}

func createAuthClient(address, username, password string) *auth.Client {
	return &auth.Client{
		Client: retry.DefaultClient,
		Header: http.Header{
			"User-Agent": []string{userAgent},
		},
		Credential: auth.StaticCredential(address, auth.Credential{
			Username: username,
			Password: password,
		}),
	}
}

func generateHtpasswd(t *testing.T, username, password string) string {
	t.Helper()
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	require.NoError(t, err)
	return fmt.Sprintf("%s:%s", username, hashedPassword)
}

func generateRandomPassword(t *testing.T, length int) string {
	t.Helper()
	password := make([]byte, length)
	for i := range password {
		randomIndex, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		require.NoError(t, err)
		password[i] = charset[randomIndex.Int64()]
	}
	return string(password)
}
