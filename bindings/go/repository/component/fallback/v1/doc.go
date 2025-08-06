// Package v1 implements a component version repository with a fallback
// mechanism. A fallback repository allows specifying a list of repository
// specifications with a priority and a prefix. Based on priority and prefix,
// the repository iterates through the list.
// In case of Get-operations, if the component version is not found, it will
// retry with the next repository in the list (with matching prefix) until it
// finds a match or exhausts.
// In case of Add-operations, it will add the component version to the first
// repository (with matching prefix). If this does not succeed, it will not
// retry.
//
// Deprecated: FallbackRepository is an implementation for the deprecated config
// type "ocm.config.ocm.software/v1". This concept of fallback resolvers is deprecated
// and only added for backwards compatibility.
// New concepts will likely be introduced in the future (contributions welcome!).
package v1
