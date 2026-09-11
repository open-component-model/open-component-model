package test

import (
	"context"
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	signingv1alpha1 "ocm.software/open-component-model/bindings/go/rsa/signing/v1alpha1"
)

// SignatureVerification describes the public key that verifies a single component signature. The algorithm
// is part of the credential consumer identity the RSA signing handler asks the credential graph for.
type SignatureVerification struct {
	Signature string
	Algorithm signingv1alpha1.SignatureAlgorithm
	PublicKey string
}

// SetupSignatureVerificationConfig creates a Secret holding an .ocmconfig that makes the controller verify the
// given signatures. Every entry contributes a signing configuration scoped to its signature name plus the
// credentials carrying the matching public key.
func SetupSignatureVerificationConfig(
	ctx context.Context,
	c client.Client,
	namespace, name string,
	verifications ...SignatureVerification,
) *corev1.Secret {
	GinkgoHelper()

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Data: map[string][]byte{
			v1alpha1.OCMConfigKey: []byte(SignatureVerificationOCMConfig(verifications...)),
		},
	}
	Expect(c.Create(ctx, secret)).To(Succeed())

	return secret
}

// SignatureVerificationOCMConfig renders the .ocmconfig content that drives signature verification.
func SignatureVerificationOCMConfig(verifications ...SignatureVerification) string {
	var builder strings.Builder
	builder.WriteString("type: generic.config.ocm.software/v1\nconfigurations:\n")

	for _, verification := range verifications {
		fmt.Fprintf(&builder, `- type: signing.config.ocm.software/v1alpha1
  signature: %[1]s
  verifier:
    type: RSASigningConfiguration/v1alpha1
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: RSA/v1alpha1
      algorithm: %[2]s
      signature: %[1]s
    credentials:
    - type: Credentials/v1
      properties:
        public_key_pem: |
%[3]s
`, verification.Signature, verification.Algorithm, indentBlock(verification.PublicKey, 10))
	}

	return builder.String()
}

// SecretOCMConfiguration builds the spec reference to an .ocmconfig Secret.
func SecretOCMConfiguration(secret *corev1.Secret) v1alpha1.OCMConfiguration {
	return v1alpha1.OCMConfiguration{
		NamespacedObjectKindReference: v1alpha1.NamespacedObjectKindReference{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "Secret",
			Name:       secret.GetName(),
			Namespace:  secret.GetNamespace(),
		},
		Policy: v1alpha1.ConfigurationPolicyPropagate,
	}
}

func indentBlock(content string, spaces int) string {
	prefix := strings.Repeat(" ", spaces)
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}

	return strings.Join(lines, "\n")
}
