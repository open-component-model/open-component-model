package e2e

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"golang.org/x/crypto/bcrypt"
	"sigs.k8s.io/kind/pkg/cluster"
	"sigs.k8s.io/kind/pkg/cmd"
)

// RegistryProvider defines the interface for an OCI registry provider.
type RegistryProvider interface {
	Setup(ctx context.Context) error
	GetURL() string
	GetCredentials() (username, password string)
	GetCertPath() string
	Teardown(ctx context.Context) error
}

// ClusterProvider defines the interface for a Kubernetes cluster provider.
type ClusterProvider interface {
	Setup(ctx context.Context) error
	GetKubeconfig() string
	Teardown(ctx context.Context) error
}

// CLIProvider defines the interface for the OCM CLI provider.
type CLIProvider interface {
	Setup(ctx context.Context) error
	Exec(ctx context.Context, args ...string) (string, error)
	GetContainerID() string
	Teardown(ctx context.Context) error
}

// ControllerProvider defines the interface for the OCM Kubernetes controllers provider.
type ControllerProvider interface {
	Setup(ctx context.Context, certsDir string) error
	Teardown(ctx context.Context) error
}

// ZotProvider implements RegistryProvider using Zot.
type ZotProvider struct {
	Config    *Config
	Container testcontainers.Container
	CertsDir  string
	WorkDir   string
}

func NewZotProvider(cfg *Config, workDir, certsDir string) *ZotProvider {
	return &ZotProvider{
		Config:   cfg,
		WorkDir:  workDir,
		CertsDir: certsDir,
	}
}

func (p *ZotProvider) Setup(ctx context.Context) error {
	// Generate Certs
	caCert, caKey, err := generateCA()
	if err != nil {
		return err
	}
	serverCert, serverKey, err := generateServerCert(caCert, caKey, "zot")
	if err != nil {
		return err
	}

	if err := os.WriteFile(filepath.Join(p.CertsDir, "ca.crt"), caCert, 0644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(p.CertsDir, "server.crt"), serverCert, 0644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(p.CertsDir, "server.key"), serverKey, 0644); err != nil {
		return err
	}

	// Zot Config
	zotConfig := map[string]interface{}{
		"distSpecVersion": "1.1.0",
		"storage": map[string]interface{}{
			"rootDirectory": "/var/lib/registry",
		},
		"http": map[string]interface{}{
			"address": "0.0.0.0",
			"port":    "5000",
			"tls": map[string]interface{}{
				"cert": "/etc/zot/certs/server.crt",
				"key":  "/etc/zot/certs/server.key",
			},
			"auth": map[string]interface{}{
				"htpasswd": map[string]interface{}{
					"path": "/etc/zot/htpasswd",
				},
			},
		},
	}
	zotConfigBytes, err := json.Marshal(zotConfig)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(p.WorkDir, "zot-config.json"), zotConfigBytes, 0644); err != nil {
		return err
	}

	// htpasswd
	password := []byte("testpassword")
	hashedPassword, err := bcrypt.GenerateFromPassword(password, bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	htpasswdContent := fmt.Sprintf("testuser:%s\n", hashedPassword)
	if err := os.WriteFile(filepath.Join(p.WorkDir, "htpasswd"), []byte(htpasswdContent), 0644); err != nil {
		return err
	}

	networkName := "kind"
	zotReq := testcontainers.ContainerRequest{
		Image:        "ghcr.io/project-zot/zot-linux-amd64:latest",
		ExposedPorts: []string{"5000/tcp"},
		Networks:     []string{networkName},
		NetworkAliases: map[string][]string{
			networkName: {"zot"},
		},
		Files: []testcontainers.ContainerFile{
			{
				HostFilePath:      filepath.Join(p.WorkDir, "zot-config.json"),
				ContainerFilePath: "/etc/zot/config.json",
				FileMode:          0644,
			},
			{
				HostFilePath:      filepath.Join(p.CertsDir, "server.crt"),
				ContainerFilePath: "/etc/zot/certs/server.crt",
				FileMode:          0644,
			},
			{
				HostFilePath:      filepath.Join(p.CertsDir, "server.key"),
				ContainerFilePath: "/etc/zot/certs/server.key",
				FileMode:          0644,
			},
			{
				HostFilePath:      filepath.Join(p.WorkDir, "htpasswd"),
				ContainerFilePath: "/etc/zot/htpasswd",
				FileMode:          0644,
			},
		},
		WaitingFor: wait.ForHTTP("/v2/").WithPort("5000/tcp").WithTLS(true, &tls.Config{InsecureSkipVerify: true}).WithStartupTimeout(120 * time.Second).WithStatusCodeMatcher(func(status int) bool {
			return status == 200 || status == 401
		}),
	}
	p.Container, err = testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: zotReq,
		Started:          true,
	})
	return err
}

func (p *ZotProvider) GetURL() string {
	return "https://zot:5000"
}

func (p *ZotProvider) GetCredentials() (string, string) {
	return "testuser", "testpassword"
}

func (p *ZotProvider) GetCertPath() string {
	return filepath.Join(p.CertsDir, "ca.crt")
}

func (p *ZotProvider) Teardown(ctx context.Context) error {
	if p.Container != nil {
		return p.Container.Terminate(ctx)
	}
	return nil
}

// KindProvider implements ClusterProvider using Kind.
type KindProvider struct {
	Config  *Config
	WorkDir string
}

func NewKindProvider(cfg *Config, workDir string) *KindProvider {
	return &KindProvider{
		Config:  cfg,
		WorkDir: workDir,
	}
}

func (p *KindProvider) Setup(ctx context.Context) error {
	provider := cluster.NewProvider(
		cluster.ProviderWithLogger(cmd.NewLogger()),
	)
	clusterName := "ocm-conformance"

	// Just in case, try to delete if exists
	_ = provider.Delete(clusterName, "")

	return provider.Create(
		clusterName,
		cluster.CreateWithWaitForReady(time.Minute*2),
		cluster.CreateWithKubeconfigPath(p.GetKubeconfig()),
	)
}

func (p *KindProvider) GetKubeconfig() string {
	return filepath.Join(p.WorkDir, "kubeconfig")
}

func (p *KindProvider) Teardown(ctx context.Context) error {
	provider := cluster.NewProvider(
		cluster.ProviderWithLogger(cmd.NewLogger()),
	)
	return provider.Delete("ocm-conformance", p.GetKubeconfig())
}

// OCMCLIProvider implements CLIProvider using a runner container.
type OCMCLIProvider struct {
	Config    *Config
	Container testcontainers.Container
	WorkDir   string
	CertsDir  string
}

func NewOCMCLIProvider(cfg *Config, workDir, certsDir string) *OCMCLIProvider {
	return &OCMCLIProvider{
		Config:   cfg,
		WorkDir:  workDir,
		CertsDir: certsDir,
	}
}

func (p *OCMCLIProvider) Setup(ctx context.Context) error {
	// A. Extract Binary
	extractReq := testcontainers.ContainerRequest{
		Image:      p.Config.OCM.Path,
		Entrypoint: []string{"/ocm", "version"},
	}
	extractC, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: extractReq,
		Started:          false,
	})
	if err != nil {
		return err
	}
	if err := extractC.Start(ctx); err != nil {
		return err
	}

	reader, err := extractC.CopyFileFromContainer(ctx, "/ocm")
	if err != nil {
		return err
	}

	ocmBinaryPath := filepath.Join(p.WorkDir, "ocm")
	outFile, err := os.Create(ocmBinaryPath)
	if err != nil {
		return err
	}
	if _, err := io.Copy(outFile, reader); err != nil {
		outFile.Close()
		return err
	}
	outFile.Close()
	if err := os.Chmod(ocmBinaryPath, 0755); err != nil {
		return err
	}
	_ = extractC.Terminate(ctx)

	// B. Start Runner
	networkName := "kind"
	runnerReq := testcontainers.ContainerRequest{
		Image:      "alpine:3.19",
		Entrypoint: []string{"sleep", "infinity"},
		Networks:   []string{networkName},
		Mounts: testcontainers.ContainerMounts{
			testcontainers.BindMount(p.WorkDir, "/workspace"),
			testcontainers.BindMount(filepath.Join(p.CertsDir, "ca.crt"), "/etc/ssl/certs/zot-ca.crt"),
			testcontainers.BindMount(ocmBinaryPath, "/usr/local/bin/ocm"),
		},
		Env: map[string]string{
			"SSL_CERT_FILE": "/etc/ssl/certs/zot-ca.crt",
		},
		WorkingDir: "/workspace",
	}
	p.Container, err = testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: runnerReq,
		Started:          true,
	})
	return err
}

func (p *OCMCLIProvider) Exec(ctx context.Context, args ...string) (string, error) {
	// Not strictly used directly here as we use docker exec from host, but good interface to have
	return "", nil
}

func (p *OCMCLIProvider) GetContainerID() string {
	return p.Container.GetContainerID()
}

func (p *OCMCLIProvider) Teardown(ctx context.Context) error {
	if p.Container != nil {
		return p.Container.Terminate(ctx)
	}
	return nil
}

// Certificate Helpers (moved from conformance_test.go)

// OCMControllerProvider implements ControllerProvider using Helm and kubectl.
type OCMControllerProvider struct {
	Config  *Config
	WorkDir string
	Cluster ClusterProvider
}

func NewOCMControllerProvider(cfg *Config, workDir string, cluster ClusterProvider) *OCMControllerProvider {
	return &OCMControllerProvider{
		Config:  cfg,
		WorkDir: workDir,
		Cluster: cluster,
	}
}

func (p *OCMControllerProvider) Setup(ctx context.Context, certsDir string) error {
	kubeconfig := p.Cluster.GetKubeconfig()

	// 1. Build and Load Docker Image
	// Build the controller image locally for E2E
	buildCmd := exec.CommandContext(ctx, "task", "-d", filepath.Join("..", "kubernetes", "controller"), "docker-build", "LOAD=true")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("task docker-build failed: %v\nOutput: %s", err, string(out))
	}

	// Load into the kind cluster
	// Cluster config in framework_test.go names the cluster "ocm-conformance"
	loadCmd := exec.CommandContext(ctx, "kind", "load", "docker-image", "ghcr.io/open-component-model/kubernetes/controller:latest", "--name", "ocm-conformance")
	if out, err := loadCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("kind load failed: %v\nOutput: %s", err, string(out))
	}

	// 2. Install Controller using Helm
	helmCmd := exec.CommandContext(ctx, "helm", "upgrade", "-i", "ocm",
		"../kubernetes/controller/chart",
		"-n", "ocm-system", "--create-namespace",
		"--set", "manager.image.repository=ghcr.io/open-component-model/kubernetes/controller",
		"--set", "manager.image.tag=latest",
		"--set", "fullnameOverride=ocm-controller",
		"--kubeconfig", kubeconfig,
	)
	if out, err := helmCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("helm install failed: %v\nOutput: %s", err, string(out))
	}

	// 2. Create Secret for Zot CA
	caCertPath := filepath.Join(certsDir, "ca.crt")
	kubectlSecret := exec.CommandContext(ctx, "kubectl", "create", "secret", "generic", "zot-ca",
		"--from-file=ca.crt="+caCertPath,
		"-n", "ocm-system",
		"--kubeconfig", kubeconfig,
	)
	if out, err := kubectlSecret.CombinedOutput(); err != nil {
		return fmt.Errorf("kubectl create secret failed: %v\nOutput: %s", err, string(out))
	}

	// 3. Patch Deployment to mount CA
	patch := `{"spec":{"template":{"spec":{"volumes":[{"name":"zot-ca","secret":{"secretName":"zot-ca"}}],"containers":[{"name":"manager","env":[{"name":"SSL_CERT_FILE","value":"/etc/ssl/certs/zot-ca.crt"}],"volumeMounts":[{"name":"zot-ca","mountPath":"/etc/ssl/certs/zot-ca.crt","subPath":"ca.crt"}]}]}}}}`
	kubectlPatch := exec.CommandContext(ctx, "kubectl", "patch", "deployment", "ocm-controller-controller-manager",
		"-n", "ocm-system",
		"-p", patch,
		"--kubeconfig", kubeconfig,
	)
	if out, err := kubectlPatch.CombinedOutput(); err != nil {
		return fmt.Errorf("kubectl patch failed: %v\nOutput: %s", err, string(out))
	}

	// 4. Wait for deployment to be ready
	kubectlWait := exec.CommandContext(ctx, "kubectl", "rollout", "status", "deployment", "ocm-controller-controller-manager",
		"-n", "ocm-system",
		"--timeout=300s",
		"--kubeconfig", kubeconfig,
	)
	if out, err := kubectlWait.CombinedOutput(); err != nil {
		describeCmd := exec.CommandContext(ctx, "kubectl", "describe", "pods", "-n", "ocm-system", "--kubeconfig", kubeconfig)
		describeOut, _ := describeCmd.CombinedOutput()
		logCmd := exec.CommandContext(ctx, "kubectl", "logs", "-n", "ocm-system", "-l", "control-plane=controller-manager", "--kubeconfig", kubeconfig)
		logOut, _ := logCmd.CombinedOutput()
		return fmt.Errorf("kubectl wait failed: %v\nOutput: %s\nDescribe: %s\nLogs: %s", err, string(out), string(describeOut), string(logOut))
	}

	// 5. Grant ClusterAdmin to the controller's ServiceAccount (for Deployer to apply resources)
	kubectlRbac := exec.CommandContext(ctx, "kubectl", "create", "clusterrolebinding", "ocm-controller-e2e-admin",
		"--clusterrole=cluster-admin",
		"--serviceaccount=ocm-system:ocm-controller-controller-manager",
		"--kubeconfig", kubeconfig,
	)
	if err := kubectlRbac.Run(); err != nil {
		// ignore ALREADY EXISTS error
	}

	return nil
}

func (p *OCMControllerProvider) Teardown(ctx context.Context) error {
	kubeconfig := p.Cluster.GetKubeconfig()
	cmd := exec.CommandContext(ctx, "helm", "uninstall", "ocm", "-n", "ocm-system", "--kubeconfig", kubeconfig)
	_ = cmd.Run()
	return nil
}

func generateCA() ([]byte, []byte, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "OCM Conformance CA",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour * 24),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})

	return certPEM, keyPEM, nil
}

func generateServerCert(caCertPEM, caKeyPEM []byte, commonName string) ([]byte, []byte, error) {
	block, _ := pem.Decode(caCertPEM)
	caCert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, nil, err
	}

	block, _ = pem.Decode(caKeyPEM)
	caKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, nil, err
	}

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject: pkix.Name{
			CommonName: commonName,
		},
		DNSNames:    []string{commonName, "localhost", "127.0.0.1"},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().Add(time.Hour * 24),
		KeyUsage:    x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, caCert, &priv.PublicKey, caKey)
	if err != nil {
		return nil, nil, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})

	return certPEM, keyPEM, nil
}
