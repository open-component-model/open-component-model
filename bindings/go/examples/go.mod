module ocm.software/open-component-model/bindings/go/examples

go 1.26.1

require (
	github.com/opencontainers/go-digest v1.0.0
	github.com/stretchr/testify v1.11.1
	ocm.software/open-component-model/bindings/go/blob v0.0.11
	ocm.software/open-component-model/bindings/go/credentials v0.0.7
	ocm.software/open-component-model/bindings/go/ctf v0.3.0
	ocm.software/open-component-model/bindings/go/descriptor/normalisation v0.0.0
	ocm.software/open-component-model/bindings/go/descriptor/runtime v0.0.0-20260310092250-61522efb19f1
	ocm.software/open-component-model/bindings/go/descriptor/v2 v2.0.1-alpha9
	ocm.software/open-component-model/bindings/go/oci v0.0.34
	ocm.software/open-component-model/bindings/go/repository v0.0.8
	ocm.software/open-component-model/bindings/go/rsa v0.0.0
	ocm.software/open-component-model/bindings/go/runtime v0.0.6
	ocm.software/open-component-model/bindings/go/signing v0.0.0
)

require (
	github.com/Masterminds/semver/v3 v3.4.0 // indirect
	github.com/cyberphone/json-canonicalization v0.0.0-20241213102144-19d51d7fe467 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/nlepage/go-tarfs v1.2.1 // indirect
	github.com/opencontainers/image-spec v1.1.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.2 // indirect
	github.com/veqryn/slog-context v0.9.0 // indirect
	go.yaml.in/yaml/v2 v2.4.3 // indirect
	golang.org/x/sync v0.19.0 // indirect
	golang.org/x/text v0.33.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	ocm.software/open-component-model/bindings/go/configuration v0.0.10 // indirect
	ocm.software/open-component-model/bindings/go/dag v0.0.6 // indirect
	oras.land/oras-go/v2 v2.6.0 // indirect
	sigs.k8s.io/yaml v1.6.0 // indirect
)

// Replace directives ensure we always test against the current local code.
replace (
	ocm.software/open-component-model/bindings/go/blob => ../blob
	ocm.software/open-component-model/bindings/go/configuration => ../configuration
	ocm.software/open-component-model/bindings/go/credentials => ../credentials
	ocm.software/open-component-model/bindings/go/ctf => ../ctf
	ocm.software/open-component-model/bindings/go/dag => ../dag
	ocm.software/open-component-model/bindings/go/descriptor/normalisation => ../descriptor/normalisation
	ocm.software/open-component-model/bindings/go/descriptor/runtime => ../descriptor/runtime
	ocm.software/open-component-model/bindings/go/descriptor/v2 => ../descriptor/v2
	ocm.software/open-component-model/bindings/go/oci => ../oci
	ocm.software/open-component-model/bindings/go/repository => ../repository
	ocm.software/open-component-model/bindings/go/rsa => ../rsa
	ocm.software/open-component-model/bindings/go/runtime => ../runtime
	ocm.software/open-component-model/bindings/go/signing => ../signing
)
