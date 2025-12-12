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
	"math/big"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/fluxcd/pkg/runtime/patch"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	signingv1alpha1 "ocm.software/open-component-model/bindings/go/rsa/signing/v1alpha1"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/kubernetes/controller/internal/status"
)

type MockComponentOptions struct {
	Client     client.Client
	Recorder   record.EventRecorder
	Info       v1alpha1.ComponentInfo
	Repository string
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

	patchHelper := patch.NewSerialPatcher(component, options.Client)

	component.Status.Component = options.Info

	Eventually(func(ctx context.Context) error {
		status.MarkReady(options.Recorder, component, "applied mock component")

		return status.UpdateStatus(ctx, patchHelper, component, options.Recorder, time.Hour, nil)
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

	credentials := map[string]string{
		"public_key_pem":  pubKey,
		"private_key_pem": string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)})),
	}

	sigBytes, err := handler.Sign(ctx, *unsignedDigest, cfg, credentials)
	Expect(err).ToNot(HaveOccurred())

	return descruntime.Signature{
		Name:      signatureName,
		Digest:    *unsignedDigest,
		Signature: sigBytes,
	}, pubKey
}
