// Package spec implements the configuration type
// "ownership.config.ocm.software", which controls whether resource uploads
// emit the asset-to-owner OCI referrer defined in ADR 0016.
//
// A top-level policy sets the default for every repository, and per-repository
// entries override that default for the OCM repositories they match. The
// feature is off by default: when no configuration is supplied, the effective
// policy is "Never".
//
// Repository entries are evaluated top-down and the first match wins, so the
// most specific entries must be listed first.
//
// For example:
//
//	type: ownership.config.ocm.software/v1alpha1
//	policy: AddIfSupported
//	repositories:
//	- repository:
//	    type: OCIRepository/v1
//	    baseUrl: ghcr.io
//	    subPath: my-org/components
//	  policy: AddIfSupported
//	- repository:
//	    type: OCIRepository/v1
//	  policy: Never
package spec
