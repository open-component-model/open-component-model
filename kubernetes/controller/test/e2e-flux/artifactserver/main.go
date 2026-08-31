// Command artifactserver is an end-to-end test harness that exercises the
// ExternalArtifact interop with real Flux controllers. It uses the SAME
// Storage/StorageServer/Artifact code as the production controller to:
//
//  1. package the content directory it is pointed at into a tar.gz on disk,
//  2. serve it over the in-cluster HTTP endpoint,
//  3. create an ExternalArtifact (source.toolkit.fluxcd.io/v1) whose
//     .status.artifact points at that tar.gz.
//
// A Flux Kustomization (sourceRef) or HelmRelease (chartRef) referencing the
// ExternalArtifact then fetches the artifact, verifies its digest, and applies
// the contents — proving real Flux consumption end to end.
//
// The content is supplied at deploy time (a mounted ConfigMap/volume passed via
// --content-dir), not baked into the binary, so a single build serves any test.
//
// This binary is only used by the e2e test; it is not part of the shipped
// controller.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	fluxmeta "github.com/fluxcd/pkg/apis/meta"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"ocm.software/open-component-model/kubernetes/controller/internal/controller/externalartifact"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "artifactserver error:", err)
		os.Exit(1)
	}
}

func run() error {
	logger := zap.New(zap.UseDevMode(true))
	ctrl.SetLogger(logger)

	var (
		contentDir string
		namespace  string
		name       string
		advertise  string
		bindAddr   string
		revision   string
		storageDir string
		tlsCert    string
		tlsKey     string
	)
	flag.StringVar(&contentDir, "content-dir", "/content", "Directory whose contents are packaged into the artifact.")
	flag.StringVar(&namespace, "namespace", "flux-system", "Namespace of the ExternalArtifact.")
	flag.StringVar(&name, "name", "ea-artifact", "Name of the ExternalArtifact.")
	flag.StringVar(&advertise, "advertise-addr", "artifactserver.flux-system.svc.cluster.local.:9091",
		"In-cluster host[:port] used to build the artifact URL.")
	flag.StringVar(&bindAddr, "bind-addr", ":9091", "Address the artifact HTTP server binds to.")
	flag.StringVar(&revision, "revision", "1.0.0", "Human-readable revision identifier.")
	flag.StringVar(&storageDir, "storage-dir", "", "Directory the artifact store is rooted at. Empty uses a temp dir; set it to a shared (RWX) volume to exercise HA.")
	flag.StringVar(&tlsCert, "tls-cert", "", "PEM certificate path; with --tls-key, serves artifacts over HTTPS.")
	flag.StringVar(&tlsKey, "tls-key", "", "PEM private key path matching --tls-cert.")
	flag.Parse()

	if _, err := os.Stat(contentDir); err != nil {
		return fmt.Errorf("content dir %q: %w", contentDir, err)
	}

	// Stage the content into a clean temp dir of regular files. This matters
	// when --content-dir is a ConfigMap volume: those mount every key as a
	// symlink (key -> ..data/key), and the production writeTarball correctly
	// skips non-regular files (it must not follow symlinks out of the tree).
	// Dereferencing into a plain tree here mirrors what the real controller
	// packages (regular files written by its OCM download/extract step).
	stageDir, err := os.MkdirTemp("", "ea-stage-*")
	if err != nil {
		return fmt.Errorf("stage dir: %w", err)
	}
	if err := stageContent(contentDir, stageDir); err != nil {
		return fmt.Errorf("stage content: %w", err)
	}

	// 1. Package + serve the staged content with the REAL controller code.
	if storageDir == "" {
		storageDir, err = os.MkdirTemp("", "ea-storage-*")
		if err != nil {
			return fmt.Errorf("storage dir: %w", err)
		}
	}
	storage, err := externalartifact.NewStorage(storageDir, advertise)
	if err != nil {
		return fmt.Errorf("new storage: %w", err)
	}

	result, err := storage.Archive(context.Background(), "Resource", namespace, name, stageDir)
	if err != nil {
		return fmt.Errorf("archive: %w", err)
	}
	artifact := storage.Artifact(result, revision)
	logger.Info("packaged artifact", "digest", artifact.Digest, "path", artifact.Path, "url", artifact.URL, "size", *artifact.Size)

	// 2. Create/patch the ExternalArtifact in-cluster.
	cfg, err := ctrl.GetConfig()
	if err != nil {
		return fmt.Errorf("get kubeconfig: %w", err)
	}
	if err := sourcev1.AddToScheme(scheme.Scheme); err != nil {
		return fmt.Errorf("add scheme: %w", err)
	}
	c, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		return fmt.Errorf("new client: %w", err)
	}

	ctx := context.Background()
	ea := &sourcev1.ExternalArtifact{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
	}
	if _, err := ctrl.CreateOrUpdate(ctx, c, ea, func() error {
		ea.Spec.SourceRef = &fluxmeta.NamespacedObjectKindReference{
			APIVersion: "delivery.ocm.software/v1alpha1",
			Kind:       "Resource",
			Name:       name,
			Namespace:  namespace,
		}
		return nil
	}); err != nil {
		return fmt.Errorf("create/update external artifact: %w", err)
	}

	// Status must be set separately (it's a subresource).
	ea.Status.Artifact = artifact
	apimeta.SetStatusCondition(&ea.Status.Conditions, metav1.Condition{
		Type:    "Ready",
		Status:  metav1.ConditionTrue,
		Reason:  "Succeeded",
		Message: fmt.Sprintf("stored artifact for revision %s", artifact.Revision),
	})
	if err := c.Status().Update(ctx, ea); err != nil {
		return fmt.Errorf("update external artifact status: %w", err)
	}
	logger.Info("created ExternalArtifact", "namespace", namespace, "name", name)

	// 3. Serve until killed.
	var serverOpts []externalartifact.StorageServerOption
	if tlsCert != "" && tlsKey != "" {
		serverOpts = append(serverOpts, externalartifact.WithTLS(tlsCert, tlsKey))
	}
	srv := externalartifact.NewStorageServer(bindAddr, storage, logger, serverOpts...)
	logger.Info("serving artifacts", "addr", bindAddr, "tls", tlsCert != "" && tlsKey != "")
	return srv.Start(signalContext())
}

func signalContext() context.Context {
	// The manager's SetupSignalHandler is process-global; a plain background
	// context plus a long-lived server is sufficient for the test pod.
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Minute)
		cancel()
	}()
	return ctx
}

// stageContent copies the logical file tree under srcRoot into dstRoot as
// regular files, following symlinks. It skips ConfigMap/Secret volume internals
// ("..data" and "..<timestamp>" entries) so a ConfigMap volume mount yields the
// clean tree its keys/items describe rather than the mount's symlink plumbing.
//
// A plain filepath.WalkDir is insufficient here: ConfigMap volumes with nested
// item paths expose the top-level directory as a symlink, and WalkDir does not
// descend into symlinked directories. This recursion opens each directory by
// path (os.ReadDir), which follows symlinks, and resolves each entry with
// os.Stat.
func stageContent(srcRoot, dstRoot string) error {
	if err := os.MkdirAll(dstRoot, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(srcRoot)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "..") { // ConfigMap/Secret volume internals
			continue
		}
		src := filepath.Join(srcRoot, name)
		dst := filepath.Join(dstRoot, name)
		// os.Stat follows symlinks (ConfigMap keys are symlinks).
		info, err := os.Stat(src)
		if err != nil {
			return err
		}
		switch {
		case info.IsDir():
			if err := stageContent(src, dst); err != nil {
				return err
			}
		case info.Mode().IsRegular():
			if err := copyFile(src, dst); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyFile(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { err = errjoin(err, in.Close()) }()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { err = errjoin(err, out.Close()) }()
	_, err = io.Copy(out, in)
	return err
}

func errjoin(a, b error) error {
	if a != nil {
		return a
	}
	return b
}
