// Package endpoints helps in constructing and tracking registered and declared types by plugins.
// A plugin would use this with a dedicated type specific constructor multiple times ( if needed ) to construct
// a chain of types that it supports. Once done, JSON marshal will create the correct structure to be returned to the
// plugin manager.
//
// In order to declare that a plugin supports a certain set of functionalities it will have to implement a given set of
// functions. These functions have set requests and return types that the plugin will need to provide. The interfaces are
// tightly coupled to these functionalities. For example for a plugin to be able to download/upload component versions
// to a repository, it would have to implement the OCMComponentVersionRepository contract. Once done, it can be used
// for these activities.
//
// A single plugin can declare multiple of these functionalities but never multiple TYPES for the same functionality.
// For example, it could provide being a credential provider of type OCI and a ComponentVersion repository of type OCI.
package endpoints
