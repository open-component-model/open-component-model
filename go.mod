module ocm.software/open-component-model/ocm

go 1.26.1

replace ocm.software/open-component-model/bindings/go/dag => ./bindings/go/dag

require (
	ocm.software/open-component-model/bindings/go/dag v0.0.6
	ocm.software/open-component-model/bindings/go/descriptor/runtime v0.0.0-20260430123712-ce7338d4fe09
	ocm.software/open-component-model/bindings/go/repository v0.0.8
)

require (
	github.com/cyberphone/json-canonicalization v0.0.0-20241213102144-19d51d7fe467 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/gobwas/glob v0.2.3 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.2 // indirect
	github.com/veqryn/slog-context v0.9.0 // indirect
	go.yaml.in/yaml/v2 v2.4.4 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/text v0.35.0 // indirect
	ocm.software/open-component-model/bindings/go/blob v0.0.11 // indirect
	ocm.software/open-component-model/bindings/go/configuration v0.0.10 // indirect
	ocm.software/open-component-model/bindings/go/credentials v0.0.7 // indirect
	ocm.software/open-component-model/bindings/go/descriptor/v2 v2.0.3-alpha3 // indirect
	ocm.software/open-component-model/bindings/go/runtime v0.0.8 // indirect
	sigs.k8s.io/yaml v1.6.0 // indirect
)
