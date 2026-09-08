package ocm

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"slices"

	v1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	ErrUnstableHash                       = errors.New("unstable hash detected")
	ErrComponentVersionIsNotNormalizeable = errors.New("component version is not normalizeable (possibly due to missing digests on component references or resources")
)

var ErrComponentVersionHashMismatch = errors.New("component version hash mismatch")

// GetObjectDataHash returns a stable 64-hex digest for a set of objects.
// Double-hash scheme:
//  1. Per object: HashMap(...) -> 32-byte SHA-256 digest.
//  2. Aggregate: sort the 32-byte digests, concatenate them (explicit []byte),
//     then SHA-256 the result. This is order-independent and unambiguous.
func GetObjectDataHash[T ctrl.Object](objects ...T) (string, error) {
	// Step 1: get fixed-size per-object digests
	digests := make([][]byte, 0, len(objects))
	for _, o := range objects {
		d, err := GetObjectHash(o)
		if err != nil {
			return "", err
		}
		digests = append(digests, d)
	}

	// Sort for order independence.
	slices.SortFunc(digests, bytes.Compare)

	// Step 2: final aggregate hash.
	// Explicit concatenation of fixed-size digests.
	sum := sha256.Sum256(bytes.Join(digests, nil))

	return hex.EncodeToString(sum[:]), nil
}

func GetObjectHash(object ctrl.Object) ([]byte, error) {
	switch o := object.(type) {
	case *v1.Secret:
		return GetSecretMapDataHash(o)
	case *v1.ConfigMap:
		return GetConfigMapDataHash(o)
	default:
		return nil, fmt.Errorf("unsupported object type for data hash calculation: %T", o)
	}
}

// GetSecretMapDataHash returns a 32-byte digest of a Secret's data.
// Empty or nil secrets hash the empty canonical form.
func GetSecretMapDataHash(s *v1.Secret) ([]byte, error) {
	if s == nil || len(s.Data) == 0 {
		return HashMap(map[string][]byte{})
	}

	return HashMap(s.Data)
}

// GetConfigMapDataHash returns a 32-byte digest of a ConfigMap's data.
// Empty or nil maps hash the empty canonical form.
func GetConfigMapDataHash(cm *v1.ConfigMap) ([]byte, error) {
	if cm == nil {
		return HashMap(map[string][]byte{})
	}
	m := make(map[string][]byte, len(cm.Data)+len(cm.BinaryData))
	for k, v := range cm.Data {
		m[k] = []byte(v)
	}
	for k, v := range cm.BinaryData {
		m[k] = v
	}
	if len(m) == 0 {
		return HashMap(map[string][]byte{})
	}

	return HashMap(m)
}

// HashMap deterministically hashes map data and returns the 32-byte SHA-256 sum.
// Keys are sorted; for each key: write key, 0x00, value, 0x00.
func HashMap(data map[string][]byte) ([]byte, error) {
	var raw bytes.Buffer
	for _, k := range slices.Sorted(maps.Keys(data)) {
		raw.WriteString(k)
		raw.WriteByte(0)
		raw.Write(data[k])
		raw.WriteByte(0)
	}
	sum := sha256.Sum256(raw.Bytes())

	return sum[:], nil
}
