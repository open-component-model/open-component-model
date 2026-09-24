package download

import (
	gitclient "github.com/go-git/go-git/v5/plumbing/transport/client"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"

	ocmhttp "ocm.software/open-component-model/bindings/go/http"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
)

// InstallHTTPClient explicitly configures go-git's process-global HTTP(S)
// registrations. Nil uses the shared OCM client defaults. Call before downloads;
// go-git's registry must not be changed concurrently with Git operations.
func InstallHTTPClient(cfg *httpv1alpha1.Config) {
	client := ocmhttp.WithHTTPSDowngradeProtection(ocmhttp.New(ocmhttp.WithConfig(cfg)))
	configured := githttp.NewClient(client)
	gitclient.InstallProtocol("http", configured)
	gitclient.InstallProtocol("https", configured)
}
