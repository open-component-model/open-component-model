package download

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/registry/remote/auth"

	"ocm.software/open-component-model/bindings/go/credentials"
	ocicredsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	testUser     = "victim"
	testPassword = "VICTIM-SHARED-REPOSITORY-PASSWORD"
)

// staticResolver resolves the hostnames it was given and reports ErrNotFound for anything
// else, standing in for the credential graph built from an ocmconfig.
type staticResolver struct {
	byHostname map[string]auth.Credential
}

func (r *staticResolver) Resolve(_ context.Context, identity runtime.Identity) (runtime.Typed, error) {
	cred, ok := r.byHostname[identity[runtime.IdentityAttributeHostname]]
	if !ok {
		return nil, credentials.ErrNotFound
	}
	return &ocicredsv1.OCICredentials{
		Type:     runtime.NewVersionedType(ocicredsv1.OCICredentialsType, ocicredsv1.Version),
		Username: cred.Username,
		Password: cred.Password,
	}, nil
}

// TestRegistryCredentialsFor covers which credentials each registry the chart pull may reach
// is served. The repository the credentials were resolved for gets them; a chart URL the
// index resolved to another host gets whatever the configuration holds for that host, and
// nothing when it holds none — never the repository's credentials.
func TestRegistryCredentialsFor(t *testing.T) {
	const repo = "https://repo.example.com/charts/mychart:1.0.0"
	repoCred := auth.Credential{Username: testUser, Password: testPassword}
	upstreamCred := auth.Credential{Username: "upstream-user", Password: "upstream-password"}

	tests := []struct {
		name         string
		helmRepo     string
		resolvedRepo string
		resolver     credentials.Resolver
		want         map[string]auth.Credential
	}{
		{
			name:         "chart served by the repository itself",
			helmRepo:     repo,
			resolvedRepo: "https://repo.example.com/charts/mychart-1.0.0.tgz",
			want:         map[string]auth.Credential{"repo.example.com": repoCred},
		},
		{
			name:         "oci repository",
			helmRepo:     "oci://registry.example.com/charts/mychart:1.0.0",
			resolvedRepo: "oci://registry.example.com/charts/mychart:1.0.0",
			want:         map[string]auth.Credential{"registry.example.com": repoCred},
		},
		{
			name:         "another registry without a resolver is served nothing",
			helmRepo:     repo,
			resolvedRepo: "oci://upstream.invalid/evil/mychart:1.0.0",
			want:         map[string]auth.Credential{"repo.example.com": repoCred},
		},
		{
			name:         "another registry absent from the configuration is served nothing",
			helmRepo:     repo,
			resolvedRepo: "oci://upstream.invalid/evil/mychart:1.0.0",
			resolver:     &staticResolver{byHostname: map[string]auth.Credential{"repo.example.com": repoCred}},
			want:         map[string]auth.Credential{"repo.example.com": repoCred},
		},
		{
			name:         "another registry is served what the configuration holds for it",
			helmRepo:     repo,
			resolvedRepo: "oci://upstream.invalid/charts/mychart:1.0.0",
			resolver:     &staticResolver{byHostname: map[string]auth.Credential{"upstream.invalid": upstreamCred}},
			want: map[string]auth.Credential{
				"repo.example.com": repoCred,
				"upstream.invalid": upstreamCred,
			},
		},
		{
			name:         "a port distinguishes registries",
			helmRepo:     "https://repo.example.com:8443/charts/mychart:1.0.0",
			resolvedRepo: "oci://repo.example.com:9999/charts/mychart:1.0.0",
			resolver:     &staticResolver{byHostname: map[string]auth.Credential{"repo.example.com": upstreamCred}},
			want: map[string]auth.Credential{
				"repo.example.com:8443": repoCred,
				"repo.example.com:9999": upstreamCred,
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			got, err := registryCredentialsFor(t.Context(), tc.resolver, tc.helmRepo, tc.resolvedRepo, testUser, testPassword)
			r.NoError(err)
			r.Equal(tc.want, got)
		})
	}
}

// TestNewRegistryClient_SendsOnlyRecordedCredentials drives the real registry client against
// a registry it holds no credentials for, as a chart URL resolved to another host produces.
func TestNewRegistryClient_SendsOnlyRecordedCredentials(t *testing.T) {
	tests := []struct {
		name     string
		recorded func(host string) map[string]auth.Credential
		wantAuth string
	}{
		{
			name: "a registry with no recorded credentials is reached unauthenticated",
			recorded: func(string) map[string]auth.Credential {
				return map[string]auth.Credential{"repo.invalid": {Username: testUser, Password: testPassword}}
			},
		},
		{
			name: "a registry with recorded credentials is reached with them",
			recorded: func(host string) map[string]auth.Credential {
				return map[string]auth.Credential{host: {Username: "upstream-user", Password: "upstream-password"}}
			},
			wantAuth: "Basic " + basicAuthHeader("upstream-user", "upstream-password"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			seen, host, client := challengingRegistry(t)

			regClient, err := newRegistryClient(client, tc.recorded(host))
			r.NoError(err)

			_, err = regClient.Resolve(host + "/charts/mychart:0.1.0")
			r.Error(err, "the registry answers 401 to every request")
			r.NotEmpty(*seen, "the registry client must have reached the host")
			last := (*seen)[len(*seen)-1]
			r.Equal(tc.wantAuth, last)
		})
	}
}
