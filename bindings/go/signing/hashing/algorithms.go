package hashing

import "github.com/opencontainers/go-digest"

const (
	HashAlgorithmSHA256Legacy = "SHA-256"
	HashAlgorithmSHA512Legacy = "SHA-512"
)

var SHAMapping = map[string]digest.Algorithm{
	HashAlgorithmSHA256Legacy: digest.SHA256,
	HashAlgorithmSHA512Legacy: digest.SHA512,
	digest.SHA256.String():    digest.SHA256,
	digest.SHA512.String():    digest.SHA512,
}

var ReverseSHAMapping = map[digest.Algorithm]string{
	digest.SHA256: HashAlgorithmSHA256Legacy,
	digest.SHA512: HashAlgorithmSHA512Legacy,
}
