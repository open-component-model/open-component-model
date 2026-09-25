// Package spec implements the specification for the versioning configuration
// type "versioning.config.ocm.software".
//
// It lets users teach OCM about additional component version schemes (for
// example calendar versioning or monotonic build numbers) using ordered,
// regex-based matchers. Without any configuration OCM continues to use loose
// semantic versioning.
package spec
