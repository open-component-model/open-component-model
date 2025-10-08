module ocm.software/open-component-model/cli

go 1.25.1

replace (
	ocm.software/open-component-model/bindings/go/constructor => ../bindings/go/constructor
	ocm.software/open-component-model/bindings/go/dag => ../bindings/go/dag
	ocm.software/open-component-model/bindings/go/descriptor/runtime => ../bindings/go/descriptor/runtime
)

require (
	github.com/Masterminds/semver/v3 v3.4.0
	github.com/jedib0t/go-pretty/v6 v6.6.8
	github.com/nlepage/go-tarfs v1.2.1
	github.com/spf13/cobra v1.10.1
	github.com/spf13/pflag v1.0.10
	github.com/stretchr/testify v1.11.1
	golang.org/x/sync v0.17.0
	golang.org/x/sys v0.36.0
	ocm.software/ocm v0.31.0
	ocm.software/open-component-model/bindings/go/blob v0.0.9
	ocm.software/open-component-model/bindings/go/configuration v0.0.9
	ocm.software/open-component-model/bindings/go/constructor v0.0.0-20251007133026-a1de8fc5798a
	ocm.software/open-component-model/bindings/go/credentials v0.0.2
	ocm.software/open-component-model/bindings/go/ctf v0.3.0
	ocm.software/open-component-model/bindings/go/dag v0.0.6
	ocm.software/open-component-model/bindings/go/descriptor/normalisation v0.0.0-20251002101013-e0cc2f41d070
	ocm.software/open-component-model/bindings/go/descriptor/runtime v0.0.0-20251009073722-3db19b40ae7d
	ocm.software/open-component-model/bindings/go/descriptor/v2 v2.0.1-alpha3
	ocm.software/open-component-model/bindings/go/input/dir v0.0.1
	ocm.software/open-component-model/bindings/go/input/file v0.0.1
	ocm.software/open-component-model/bindings/go/input/utf8 v0.0.0-20251002101013-e0cc2f41d070
	ocm.software/open-component-model/bindings/go/oci v0.0.9
	ocm.software/open-component-model/bindings/go/plugin v0.0.7
	ocm.software/open-component-model/bindings/go/repository v0.0.2
	ocm.software/open-component-model/bindings/go/rsa v0.0.0-20251007091609-0f6fd5aa0c28
	ocm.software/open-component-model/bindings/go/runtime v0.0.2
	oras.land/oras-go/v2 v2.6.0
	sigs.k8s.io/yaml v1.6.0
)

require (
	github.com/bahlo/generic-list-go v0.2.0 // indirect
	github.com/buger/jsonparser v1.1.1 // indirect
	github.com/chainguard-dev/git-urls v1.0.2 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.7 // indirect
	github.com/cyberphone/json-canonicalization v0.0.0-20241213102144-19d51d7fe467 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/docker/cli v28.3.3+incompatible // indirect
	github.com/docker/docker-credential-helpers v0.9.3 // indirect
	github.com/fxamacker/cbor/v2 v2.9.0 // indirect
	github.com/gabriel-vasile/mimetype v1.4.10 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/gowebpki/jcs v1.0.1 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/hashicorp/go-retryablehttp v0.7.8 // indirect
	github.com/hashicorp/go-rootcerts v1.0.2 // indirect
	github.com/hashicorp/go-secure-stdlib/strutil v0.1.2 // indirect
	github.com/hashicorp/vault-client-go v0.4.3 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/invopop/jsonschema v0.13.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/klauspost/compress v1.18.0 // indirect
	github.com/klauspost/pgzip v1.2.6 // indirect
	github.com/mailru/easyjson v0.9.1 // indirect
	github.com/mandelsoft/filepath v0.0.0-20240223090642-3e2777258aa3 // indirect
	github.com/mandelsoft/goutils v0.0.0-20241005173814-114fa825bbdc // indirect
	github.com/mandelsoft/logging v0.0.0-20240618075559-fdca28a87b0a // indirect
	github.com/mandelsoft/vfs v0.4.4 // indirect
	github.com/marstr/guid v1.1.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-runewidth v0.0.16 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.3-0.20250322232337-35a7c28c31ee // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.1.1 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/ryanuber/go-glob v1.0.0 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.2 // indirect
	github.com/sirupsen/logrus v1.9.3 // indirect
	github.com/texttheater/golang-levenshtein v1.0.1 // indirect
	github.com/tonglil/buflogr v1.1.1 // indirect
	github.com/ulikunitz/xz v0.5.13 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasttemplate v1.2.2 // indirect
	github.com/veqryn/slog-context v0.8.0 // indirect
	github.com/wk8/go-ordered-map/v2 v2.1.8 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	go.yaml.in/yaml/v2 v2.4.3 // indirect
	golang.org/x/net v0.43.0 // indirect
	golang.org/x/term v0.35.0 // indirect
	golang.org/x/text v0.29.0 // indirect
	golang.org/x/time v0.12.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	gotest.tools/v3 v3.5.2 // indirect
	k8s.io/apimachinery v0.34.0 // indirect
	k8s.io/klog/v2 v2.130.1 // indirect
	k8s.io/utils v0.0.0-20250604170112-4c0f3b243397 // indirect
	ocm.software/open-component-model/bindings/go/signing v0.0.0-20250915165427-710b0c881b3c // indirect
	sigs.k8s.io/json v0.0.0-20241014173422-cfa47c3a1cc8 // indirect
	sigs.k8s.io/randfill v1.0.0 // indirect
	sigs.k8s.io/structured-merge-diff/v6 v6.3.0 // indirect
)
