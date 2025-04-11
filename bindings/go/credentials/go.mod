module ocm.software/open-component-model/bindings/go/credentials

go 1.24.2

replace ocm.software/open-component-model/bindings/go/dag => ../dag

require (
	ocm.software/open-component-model/bindings/go/dag v0.0.0-00010101000000-000000000000
	ocm.software/open-component-model/bindings/go/runtime v0.0.0-20250411085310-f26479cdcc62
)

require (
	github.com/cyberphone/json-canonicalization v0.0.0-20241213102144-19d51d7fe467 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
	sigs.k8s.io/yaml v1.4.0 // indirect
)
