package credentials

import (
	"net/http"

	"github.com/oras-project/oras-go/v3/registry/remote/auth"

	v1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	identityv1 "ocm.software/open-component-model/bindings/go/oci/spec/identity/v1"
)

// AuthClient builds an ORAS auth client for a registry from the shared HTTP
// client, the consumer identity and the credentials. It keeps the distribution
// spec token flow for basic credentials instead of attempting OAuth2 first,
// which not all registries implement. userAgent is sent on authenticated
// requests.
//
// It is the single place that wires the auth client, so the component version
// provider and the component lister stay in step.
func AuthClient(httpClient *http.Client, identity *identityv1.OCIRegistryIdentity, credentials *v1.OCICredentials, userAgent string) *auth.Client {
	header := http.Header{"User-Agent": {userAgent}}
	return &auth.Client{
		Client:         httpClient,
		Cache:          auth.NewCache(),
		CredentialFunc: CredentialFunc(identity, credentials),
		Header:         header,
		TokenFetcher:   auth.NewCompositeTokenFetcher(httpClient, header, "", true),
	}
}
