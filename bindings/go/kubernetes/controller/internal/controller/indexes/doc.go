// Package indexes registers field indexes shared by multiple controllers.
//
// Manager bootstrap code must register each shared index exactly once before
// setting up its consuming controllers and before starting the manager.
// Standalone controller test harnesses own the same prerequisite.
package indexes
