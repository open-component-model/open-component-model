module ocm.software/open-component-model/cli

go 1.25.1

require (
	github.com/Masterminds/semver/v3 v3.4.0
	github.com/jedib0t/go-pretty/v6 v6.7.5
	github.com/nlepage/go-tarfs v1.2.1
	github.com/spf13/cobra v1.10.1
	github.com/spf13/pflag v1.0.10
	github.com/stretchr/testify v1.11.1
	golang.org/x/sync v0.18.0
	golang.org/x/sys v0.38.0
	gopkg.in/yaml.v3 v3.0.1
	ocm.software/open-component-model/bindings/go/blob v0.0.10
	ocm.software/open-component-model/bindings/go/configuration v0.0.9
	ocm.software/open-component-model/bindings/go/constructor v0.0.2
	ocm.software/open-component-model/bindings/go/credentials v0.0.5
	ocm.software/open-component-model/bindings/go/ctf v0.3.0
	ocm.software/open-component-model/bindings/go/dag v0.0.6
	ocm.software/open-component-model/bindings/go/descriptor/normalisation v0.0.0-20251204125915-cb295d402227
	ocm.software/open-component-model/bindings/go/descriptor/runtime v0.0.0-20251204125915-cb295d402227
	ocm.software/open-component-model/bindings/go/descriptor/v2 v2.0.1-alpha4
	ocm.software/open-component-model/bindings/go/input/dir v0.0.2
	ocm.software/open-component-model/bindings/go/input/file v0.0.3
	ocm.software/open-component-model/bindings/go/input/utf8 v0.0.0-20251204125915-cb295d402227
	ocm.software/open-component-model/bindings/go/oci v0.0.17
	ocm.software/open-component-model/bindings/go/plugin v0.0.12
	ocm.software/open-component-model/bindings/go/repository v0.0.4
	ocm.software/open-component-model/bindings/go/rsa v0.0.0-20251204125915-cb295d402227
	ocm.software/open-component-model/bindings/go/runtime v0.0.4
	ocm.software/open-component-model/bindings/go/signing v0.0.0-20251204125915-cb295d402227
	oras.land/oras-go/v2 v2.6.0
	sigs.k8s.io/yaml v1.6.0
)

replace ocm.software/open-component-model/bindings/go/constructor => ../bindings/go/constructor

require (
	github.com/bahlo/generic-list-go v0.2.0 // indirect
	github.com/buger/jsonparser v1.1.1 // indirect
	github.com/clipperhouse/stringish v0.1.1 // indirect
	github.com/clipperhouse/uax29/v2 v2.3.0 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.7 // indirect
	github.com/cyberphone/json-canonicalization v0.0.0-20241213102144-19d51d7fe467 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/gabriel-vasile/mimetype v1.4.11 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/gobwas/glob v0.2.3 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/invopop/jsonschema v0.13.0 // indirect
	github.com/mailru/easyjson v0.9.1 // indirect
	github.com/mattn/go-runewidth v0.0.19 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.1.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.2 // indirect
	github.com/veqryn/slog-context v0.8.0 // indirect
	github.com/wk8/go-ordered-map/v2 v2.1.8 // indirect
	go.yaml.in/yaml/v2 v2.4.3 // indirect
	golang.org/x/text v0.31.0 // indirect
)
