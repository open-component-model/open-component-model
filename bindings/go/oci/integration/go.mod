module ocm.software/open-component-model/bindings/go/oci/integration

go 1.25.4

require (
	github.com/nlepage/go-tarfs v1.2.1
	github.com/opencontainers/go-digest v1.0.0
	github.com/opencontainers/image-spec v1.1.1
	github.com/stretchr/testify v1.11.1
	github.com/testcontainers/testcontainers-go v0.40.0
	github.com/testcontainers/testcontainers-go/modules/registry v0.40.0
	golang.org/x/crypto v0.45.0
	ocm.software/open-component-model/bindings/go/blob v0.0.10
	ocm.software/open-component-model/bindings/go/ctf v0.3.0
	ocm.software/open-component-model/bindings/go/descriptor/runtime v0.0.0-20251204125915-cb295d402227
	ocm.software/open-component-model/bindings/go/descriptor/v2 v2.0.1-alpha4
	ocm.software/open-component-model/bindings/go/oci v0.0.17
	ocm.software/open-component-model/bindings/go/repository v0.0.4
	ocm.software/open-component-model/bindings/go/runtime v0.0.4
	oras.land/oras-go/v2 v2.6.0
)

// Replace include to make sure we are always testing the current code instead of a version.
// It is irrelevant for us that integration tests cannot be consumed by other modules.
replace ocm.software/open-component-model/bindings/go/oci => ../

require (
	dario.cat/mergo v1.0.2 // indirect
	github.com/Azure/go-ansiterm v0.0.0-20250102033503-faa5f7b0171c // indirect
	github.com/Masterminds/semver/v3 v3.4.0 // indirect
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/containerd/errdefs v1.0.0 // indirect
	github.com/containerd/errdefs/pkg v0.3.0 // indirect
	github.com/containerd/log v0.1.0 // indirect
	github.com/containerd/platforms v0.2.1 // indirect
	github.com/cpuguy83/dockercfg v0.3.2 // indirect
	github.com/creack/pty v1.1.24 // indirect
	github.com/cyberphone/json-canonicalization v0.0.0-20241213102144-19d51d7fe467 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/distribution/reference v0.6.0 // indirect
	github.com/docker/docker v28.5.2+incompatible // indirect
	github.com/docker/go-connections v0.6.0 // indirect
	github.com/docker/go-units v0.5.0 // indirect
	github.com/ebitengine/purego v0.9.1 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/klauspost/compress v1.18.2 // indirect
	github.com/lufia/plan9stats v0.0.0-20251013123823-9fd1530e3ec3 // indirect
	github.com/magiconair/properties v1.8.10 // indirect
	github.com/moby/docker-image-spec v1.3.1 // indirect
	github.com/moby/go-archive v0.1.0 // indirect
	github.com/moby/patternmatcher v0.6.0 // indirect
	github.com/moby/sys/sequential v0.6.0 // indirect
	github.com/moby/sys/user v0.4.0 // indirect
	github.com/moby/sys/userns v0.1.0 // indirect
	github.com/moby/term v0.5.2 // indirect
	github.com/morikuni/aec v1.0.0 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/power-devops/perfstat v0.0.0-20240221224432-82ca36839d55 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.2 // indirect
	github.com/shirou/gopsutil/v4 v4.25.11 // indirect
	github.com/sirupsen/logrus v1.9.3 // indirect
	github.com/tklauser/go-sysconf v0.3.16 // indirect
	github.com/tklauser/numcpus v0.11.0 // indirect
	github.com/veqryn/slog-context v0.8.0 // indirect
	github.com/yusufpapurcu/wmi v1.2.4 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.63.0 // indirect
	go.opentelemetry.io/otel v1.38.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.38.0 // indirect
	go.opentelemetry.io/otel/metric v1.38.0 // indirect
	go.opentelemetry.io/otel/trace v1.38.0 // indirect
	go.opentelemetry.io/proto/otlp v1.8.0 // indirect
	go.yaml.in/yaml/v2 v2.4.3 // indirect
	golang.org/x/sync v0.18.0 // indirect
	golang.org/x/sys v0.38.0 // indirect
	golang.org/x/text v0.31.0 // indirect
	golang.org/x/time v0.14.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20250908214217-97024824d090 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250908214217-97024824d090 // indirect
	google.golang.org/grpc v1.76.0 // indirect
	google.golang.org/protobuf v1.36.10 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	sigs.k8s.io/yaml v1.6.0 // indirect
)
