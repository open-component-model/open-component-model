// Package contracts contains all the interface contracts for various capabilities of plugins.
//
// Base Plugin:
//
//	All plugins should implement this basic interface. It contains the Ping method which is used as a Health Check.
//	This interface is also used during finding plugins. All plugins are collected as base plugins and then are
//	type asserted into the right interface from there.
//
// ComponentVersionRepository:
//
//	A plugin offering finding and retrieving component versions and local resources.
package contracts
