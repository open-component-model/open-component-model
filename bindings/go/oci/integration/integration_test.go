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

func Test_Integration_OCIRepository(t *testing.T) {
	const DistributionRegistry = "registry:2.8.3"
	ctx := t.Context()
	r := require.New(t)
	t.Logf("Running integration tests for OCI")

	user, password := "ocm", generateRandomPassword(t, 20)
	htpasswd := generateHtpasswd(t, user, password)

	t.Logf("starting registry %q ...", DistributionRegistry)
	registryContainer, err := registry.Run(ctx, DistributionRegistry, registry.WithHtpasswd(htpasswd))
	t.Cleanup(func() {
		r.NoError(testcontainers.TerminateContainer(registryContainer))
	})
	r.NoError(err)
	t.Logf("registry started!")

	registryAddress, err := registryContainer.HostAddress(ctx)
	r.NoError(err)

	reference := func(image string) string {
		return fmt.Sprintf("%s/%s", registryAddress, image)
	}

	client := &auth.Client{
		Client: retry.DefaultClient,
		Header: http.Header{
			"User-Agent": []string{"ocm.software"},
		},
		Credential: auth.StaticCredential(registryAddress, auth.Credential{
			Username: user,
			Password: password,
		}),
	}

	t.Run("test basic connectivity resolution and resolution failure", func(t *testing.T) {
		ctx := t.Context()
		r := require.New(t)

		resolver := oci.NewURLPathResolver(registryAddress)
		resolver.SetClient(client)
		resolver.PlainHTTP = true

		ref := reference("target:latest")

		store, err := resolver.StoreForReference(ctx, ref)
		r.NoError(err)

		_, err = store.Resolve(ctx, ref)
		r.ErrorIs(err, errdef.ErrNotFound)
		r.ErrorContains(err, fmt.Sprintf("%s: not found", ref))
	})

}

func generateHtpasswd(t *testing.T, username, password string) string {
	t.Helper()
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	require.NoError(t, err)
	return fmt.Sprintf("%s:%s", username, string(hashedPassword))
}

const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*()-_=+[]{}<>?"

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
