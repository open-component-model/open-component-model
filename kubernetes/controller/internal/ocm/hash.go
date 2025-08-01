package ocm

import (
	"bytes"
	"crypto"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"slices"

	"ocm.software/ocm/api/ocm/compdesc"

	v1 "k8s.io/api/core/v1"
	ocmctx "ocm.software/ocm/api/ocm"
	ocmv1 "ocm.software/ocm/api/ocm/compdesc/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"
)

var ErrComponentVersionHashMismatch = errors.New("component version hash mismatch")

// CompareCachedAndLiveHashes compares the normalized hashes of a cached component version
// and the corresponding live version in the repository.
//
// It performs the following steps:
//  1. Looks up the live component version from the given repository.
//  2. Computes a normalized hash for both the cached descriptor and the live descriptor
//     using the specified normalization algorithm and hash function.
//  3. Compares the two hashes. If they differ, returns ErrComponentVersionHashMismatch.
//  4. If they match, returns a DigestSpec with the hash metadata.
func CompareCachedAndLiveHashes(
	currentComponentVersion ocmctx.ComponentVersionAccess,
	liveRepo ocmctx.Repository,
	component, version string,
	normAlgo compdesc.NormalisationAlgorithm,
	hash crypto.Hash,
) (_ *ocmv1.DigestSpec, err error) {
	liveCV, err := liveRepo.LookupComponentVersion(component, version)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup live component version to compare with current state: %w", err)
	}
	defer func() {
		err = errors.Join(err, liveCV.Close())
	}()

	// cached version from session
	cachedDesc := currentComponentVersion.GetDescriptor()
	if err := cachedDesc.IsNormalizeable(); err != nil {
		return nil, fmt.Errorf("cached component version is not normalizeable: %w", err)
	}
	cachedHash, err := compdesc.Hash(cachedDesc, normAlgo, hash.New())
	if err != nil {
		return nil, fmt.Errorf("failed to hash cached component version: %w", err)
	}

	liveDesc := liveCV.GetDescriptor()
	if err := liveDesc.IsNormalizeable(); err != nil {
		return nil, fmt.Errorf("live component version is not normalizeable: %w", err)
	}
	liveHash, err := compdesc.Hash(liveDesc, normAlgo, hash.New())
	if err != nil {
		return nil, fmt.Errorf("failed to hash live component version: %w", err)
	}

	if cachedHash != liveHash {
		return nil, fmt.Errorf("%w: %s != %s", ErrComponentVersionHashMismatch, hash, liveHash)
	}

	return &ocmv1.DigestSpec{
		HashAlgorithm:          hash.String(),
		NormalisationAlgorithm: normAlgo,
		Value:                  cachedHash,
	}, nil
}

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
