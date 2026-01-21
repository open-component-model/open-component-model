package applyset

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ComputeID computes an ApplySet identifier for a given parent appliedObject.
// Format: base64(sha256(<name>.<namespace>.<kind>.<group>)), using the URL safe encoding of RFC4648.
// @see https://github.com/kubernetes/enhancements/blob/master/keps/sig-cli/3659-kubectl-apply-prune/README.md#applyset-identification
func ComputeID(parent client.Object) string {
	gvk := parent.GetObjectKind().GroupVersionKind()
	unencoded := strings.Join([]string{
		parent.GetName(),
		parent.GetNamespace(),
		gvk.Kind,
		gvk.Group,
	}, ApplySetIDPartDelimiter)

	hashed := sha256.Sum256([]byte(unencoded))
	b64 := base64.RawURLEncoding.EncodeToString(hashed[:])

	return fmt.Sprintf(V1ApplySetIdFormat, b64)
}
