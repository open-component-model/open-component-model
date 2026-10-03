package test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// DefaultEnvTestVersion is the Kubernetes version used by envtest when
// ENVTEST_K8S_VERSION is not set, so plain `go test ./...` sweeps work without
// task-provided environment. Should be in sync with ENVTEST_K8S_VERSION
// in kubernetes/controller/.env (both share the envtest datasource).
// renovate: datasource=custom.envtest depName=envtest
const DefaultEnvTestVersion = "1.36.0"

// ControllerRoot returns the absolute path of the controller tree, resolved
// from this file so it is independent of the test package working directory.
func ControllerRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

// NewEnvTest returns an envtest.Environment that self-provisions its control
// plane binaries (kube-apiserver and etcd). The binaries are downloaded into
// <controller>/bin/k8s/<version>-<os>-<arch> on first use and reused after,
// which is the same location setup-envtest uses when invoked via task.
// No task-level KUBEBUILDER_ASSETS setup is required. Callers may add fields
// (for example CRDs) before Start.
func NewEnvTest() *envtest.Environment {
	version := os.Getenv("ENVTEST_K8S_VERSION")
	if version == "" {
		version = DefaultEnvTestVersion
	}
	return &envtest.Environment{
		CRDDirectoryPaths:           []string{filepath.Join(ControllerRoot(), "config", "crd", "bases")},
		ErrorIfCRDPathMissing:       true,
		BinaryAssetsDirectory:       filepath.Join(ControllerRoot(), "bin", "k8s", fmt.Sprintf("%s-%s-%s", version, runtime.GOOS, runtime.GOARCH)),
		DownloadBinaryAssets:        true,
		DownloadBinaryAssetsVersion: version,
	}
}
