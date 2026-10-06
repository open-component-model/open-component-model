package test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	gpgcredentialsv1 "ocm.software/open-component-model/bindings/go/gpg/spec/credentials/v1alpha1"
	gpgsigningv1alpha1 "ocm.software/open-component-model/bindings/go/gpg/spec/signing/v1alpha1"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/status"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	signingv1alpha1 "ocm.software/open-component-model/bindings/go/rsa/signing/v1alpha1"
	rsacredentialsv1 "ocm.software/open-component-model/bindings/go/rsa/spec/credentials/v1"
)

type MockComponentOptions struct {
	Client             client.Client
	Recorder           record.EventRecorder
	Info               v1alpha1.ComponentInfo
	Repository         string
	EffectiveOCMConfig []v1alpha1.OCMConfiguration
}

func MockComponent(
	ctx context.Context,
	name, namespace string,
	options *MockComponentOptions,
) *v1alpha1.Component {
	GinkgoHelper()

	component := &v1alpha1.Component{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1alpha1.ComponentSpec{
			RepositoryRef: corev1.LocalObjectReference{
				Name: options.Repository,
			},
			Component: options.Info.Component,
		},
	}
	Expect(options.Client.Create(ctx, component)).To(Succeed())

	old := component.DeepCopy()

	component.Status.Component = options.Info
	component.Status.EffectiveOCMConfig = options.EffectiveOCMConfig

	status.MarkReady(options.Recorder, component, "applied mock component")
	component.SetObservedGeneration(component.GetGeneration())
	Expect(options.Client.Status().Patch(ctx, component, client.MergeFrom(old))).To(Succeed())

	Eventually(func(ctx context.Context) error {
		c := &v1alpha1.Component{}
		Expect(options.Client.Get(ctx, client.ObjectKeyFromObject(component), c)).To(Succeed())

		if apimeta.IsStatusConditionTrue(c.GetConditions(), v1alpha1.ReadyCondition) {
			return nil
		}

		return errors.New("component is not ready")
	}, "15s").WithContext(ctx).Should(Succeed())

	return component
}

func SignComponent(ctx context.Context, signatureName string, signAlgo signingv1alpha1.SignatureAlgorithm, normalised []byte, pm *manager.PluginManager) (descruntime.Signature, string) {
	GinkgoHelper()

	cfg := &signingv1alpha1.Config{
		SignatureAlgorithm:      signAlgo,
		SignatureEncodingPolicy: signingv1alpha1.SignatureEncodingPolicyPlain,
	}

	handler, err := pm.SigningRegistry.GetPlugin(ctx, cfg)
	Expect(err).ToNot(HaveOccurred())

	h := crypto.SHA512.New()
	_, err = h.Write(normalised)
	Expect(err).ToNot(HaveOccurred())
	freshDigest := h.Sum(nil)

	// Create unsigned digest
	unsignedDigest := &descruntime.Digest{
		HashAlgorithm:          crypto.SHA512.String(),
		NormalisationAlgorithm: v4alpha1.Algorithm,
		Value:                  hex.EncodeToString(freshDigest),
	}

	// Generate RSA key pair
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	Expect(err).ToNot(HaveOccurred())

	// Self-signed cert
	n, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	Expect(err).ToNot(HaveOccurred())
	tmpl := &x509.Certificate{
		SerialNumber:          n,
		Subject:               pkix.Name{CommonName: "signer"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &k.PublicKey, k)
	Expect(err).ToNot(HaveOccurred())
	cert, err := x509.ParseCertificate(der)
	Expect(err).ToNot(HaveOccurred())

	pubKey := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}))

	credentials := &rsacredentialsv1.RSACredentials{
		Type:          rsacredentialsv1.VersionedType,
		PublicKeyPEM:  pubKey,
		PrivateKeyPEM: string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)})),
	}

	sigBytes, err := handler.Sign(ctx, *unsignedDigest, cfg, credentials)
	Expect(err).ToNot(HaveOccurred())

	return descruntime.Signature{
		Name:      signatureName,
		Digest:    *unsignedDigest,
		Signature: sigBytes,
	}, pubKey
}

// SignComponentGPG generates an ephemeral GPG key with the gpg binary, signs the normalised
// descriptor bytes through the GPG signing handler, and returns the resulting Signature plus the
// armored public key. Keys are generated by gpg itself (not an in-process OpenPGP library) so they
// are always accepted by the gpg binary the signing handler shells out to.
func SignComponentGPG(ctx context.Context, signatureName string, normalised []byte, pm *manager.PluginManager) (descruntime.Signature, string) {
	GinkgoHelper()

	cfg := &gpgsigningv1alpha1.Config{}

	handler, err := pm.SigningRegistry.GetPlugin(ctx, cfg)
	Expect(err).ToNot(HaveOccurred())

	h := crypto.SHA256.New()
	_, err = h.Write(normalised)
	Expect(err).ToNot(HaveOccurred())
	freshDigest := h.Sum(nil)

	unsignedDigest := &descruntime.Digest{
		HashAlgorithm:          crypto.SHA256.String(),
		NormalisationAlgorithm: v4alpha1.Algorithm,
		Value:                  hex.EncodeToString(freshDigest),
	}

	priv, pub := generateGPGKeyPair(ctx)

	credentials := &gpgcredentialsv1.GPGCredentials{
		PrivateKeyPGP: priv,
		PublicKeyPGP:  pub,
	}

	sigBytes, err := handler.Sign(ctx, *unsignedDigest, cfg, credentials)
	Expect(err).ToNot(HaveOccurred())

	return descruntime.Signature{
		Name:      signatureName,
		Digest:    *unsignedDigest,
		Signature: sigBytes,
	}, pub
}

// generateGPGKeyPair creates an ephemeral RSA signing key with the gpg binary in an isolated home
// directory and returns the armored secret and public keys.
func generateGPGKeyPair(ctx context.Context) (secret, public string) {
	GinkgoHelper()

	// Not GinkgoT().TempDir(): its long path can overflow the Unix socket path limit of gpg-agent.
	home, err := os.MkdirTemp("", "ocm-controller-gpg-")
	Expect(err).ToNot(HaveOccurred())
	DeferCleanup(func() {
		_ = exec.CommandContext(ctx, "gpgconf", "--homedir", home, "--kill", "all").Run()
		_ = os.RemoveAll(home)
	})

	gpg := func(args ...string) string {
		base := []string{"--batch", "--homedir", home, "--pinentry-mode", "loopback", "--passphrase", ""}
		// #nosec G204 -- args are test-controlled constants, home is an os.MkdirTemp path.
		cmd := exec.CommandContext(ctx, "gpg", append(base, args...)...)
		out, err := cmd.CombinedOutput()
		Expect(err).ToNot(HaveOccurred(), "gpg %v: %s", args, string(out))
		return string(out)
	}

	gpg("--quick-gen-key", "OCM Controller Test <ocm@e2e.test>", "rsa2048", "sign", "never")

	var fpr string
	for _, line := range strings.Split(gpg("--with-colons", "--list-secret-keys"), "\n") {
		if fields := strings.Split(line, ":"); fields[0] == "fpr" && len(fields) > 9 {
			fpr = fields[9]
			break
		}
	}
	Expect(fpr).ToNot(BeEmpty())

	return gpg("--armor", "--export-secret-keys", fpr), gpg("--armor", "--export", fpr)
}
