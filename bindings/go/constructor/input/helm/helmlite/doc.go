// Package helmlite is a lightweight copy of the HELM packaging and loading functionality.
// It is inspired by reflectlite and only contains introspection functionality for charts and not the rest of the
// helm ecosystem.
// The functionality itself is completely in parity to or equivalent to the original helm go module available at
// https://github.com/helm/helm/ (version v3.17.3)
//
// Note that we maintain this package in parallel to the original helm package because it allows us to cheaply
// work with charts without having to draw in other dependencies that we would otherwise not need.
// An example thereof would be the kubernetes dependencies which we dont need for packaging.
package helmlite
