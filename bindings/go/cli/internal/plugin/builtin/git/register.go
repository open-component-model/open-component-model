package git

import (
	"fmt"

	gitclient "github.com/go-git/go-git/v5/plumbing/transport/client"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	gitrepository "ocm.software/open-component-model/bindings/go/git/repository"
	gitcreds "ocm.software/open-component-model/bindings/go/git/spec/credentials"
	httpclient "ocm.software/open-component-model/bindings/go/http"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialrepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/digestprocessor"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/resource"
)

// Register wires the Git resource repository, digest processor and credential
// scheme into the CLI plugin registries.
//
// httpConfig reaches Git over the go-git protocol registry, because go-git
// takes no HTTP client per clone or fetch. The registry is process global, so
// only the CLI writes to it, and it is the CLI that owns the process-wide HTTP
// configuration. Git transports other than http(s) are untouched.
//
// The installed client carries the configured timeouts, retries and TLS
// verification. Its transport is a chain rather than a plain *http.Transport,
// which go-git requires when a per-operation CA bundle is set; the CLI sets
// none, so add the bundle to this client rather than to the Git options.
func Register(resourcePluginRegistry *resource.ResourceRegistry,
	digestProcessorRegistry *digestprocessor.RepositoryRegistry,
	credentialRepository *credentialrepository.RepositoryRegistry,
	filesystemConfig *filesystemv1alpha1.Config,
	httpConfig *httpv1alpha1.Config,
) error {
	var opts []gitrepository.Option
	if filesystemConfig.TempFolder != nil {
		opts = append(opts, gitrepository.WithTempDir(*filesystemConfig.TempFolder))
	}

	transport := githttp.NewClient(httpclient.New(httpclient.WithConfig(httpConfig)))
	gitclient.InstallProtocol("http", transport)
	gitclient.InstallProtocol("https", transport)

	credentialRepository.Register(gitcreds.Scheme)

	repository := gitrepository.NewResourceRepository(opts...)
	if err := resourcePluginRegistry.RegisterInternalResourcePlugin(repository); err != nil {
		return fmt.Errorf("could not register git resource repository plugin: %w", err)
	}
	if err := digestProcessorRegistry.RegisterInternalDigestProcessorPlugin(repository); err != nil {
		return fmt.Errorf("could not register git digest processor plugin: %w", err)
	}

	return nil
}
