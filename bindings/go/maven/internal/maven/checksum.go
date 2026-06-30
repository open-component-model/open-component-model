package maven

import (
	"crypto/sha1"
	"fmt"
	"strings"
)

// ChecksumExtension is the suffix of the Maven SHA-1 checksum sibling file.
const ChecksumExtension = ".sha1"

// SHA1Hex returns the lowercase hex-encoded SHA-1 digest of data.
func SHA1Hex(data []byte) string {
	return fmt.Sprintf("%x", sha1.Sum(data))
}

// ParseSHA1File extracts the hex digest from the body of a Maven ".sha1" file.
// Maven checksum files contain the lowercase hex digest, sometimes followed by
// whitespace and the file name; only the first whitespace-delimited token is the
// digest. Returns "" when the body has no token.
func ParseSHA1File(body []byte) string {
	fields := strings.Fields(string(body))
	if len(fields) == 0 {
		return ""
	}
	return strings.ToLower(fields[0])
}

// VerifySHA1 returns an error when the SHA-1 of data does not match expectedHex
// (compared case-insensitively).
func VerifySHA1(data []byte, expectedHex string) error {
	got := SHA1Hex(data)
	if !strings.EqualFold(got, expectedHex) {
		return fmt.Errorf("SHA-1 digest mismatch: expected %s, found %s", strings.ToLower(expectedHex), got)
	}
	return nil
}
