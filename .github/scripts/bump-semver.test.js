import assert from "assert";
import { parseVersion } from "./semver-utils.js";
import { bumpVersion } from "./bump-semver.js";

// ----------------------------------------------------------
// parseVersion tests
// ----------------------------------------------------------
console.log("Testing parseVersion...");

assert.deepStrictEqual(
    parseVersion("v1.2.3"),
    { major: 1, minor: 2, patch: 3, prerelease: "" },
    "Should parse version with 'v' prefix"
);

assert.deepStrictEqual(
    parseVersion("1.2.3"),
    { major: 1, minor: 2, patch: 3, prerelease: "" },
    "Should parse version without 'v' prefix"
);

assert.deepStrictEqual(
    parseVersion("v1.2.3-rc.1"),
    { major: 1, minor: 2, patch: 3, prerelease: "rc.1" },
    "Should parse version with prerelease"
);

assert.deepStrictEqual(
    parseVersion("v10.20.30"),
    { major: 10, minor: 20, patch: 30, prerelease: "" },
    "Should parse large version numbers"
);

assert.deepStrictEqual(
    parseVersion("v0.0.0"),
    { major: 0, minor: 0, patch: 0, prerelease: "" },
    "Should parse zero version"
);

assert.deepStrictEqual(
    parseVersion("v0.1.0"),
    { major: 0, minor: 1, patch: 0, prerelease: "" },
    "Should parse minor version numbers"
);

assert.deepStrictEqual(
    parseVersion("\"v0.1.0\""),
    { major: 0, minor: 1, patch: 0, prerelease: "" },
    "Should parse values that are quoted"
);

assert.deepStrictEqual(
    parseVersion("v1.2.3-alpha.1+build.123"),
    { major: 1, minor: 2, patch: 3, prerelease: "alpha.1+build.123" },
    "Should parse version with build metadata"
);

// ----------------------------------------------------------
// parseVersion error cases
// ----------------------------------------------------------
console.log("Testing parseVersion error handling...");

assert.throws(() => {
    parseVersion("");
}, /version is required/, "Empty version should throw");

assert.throws(() => {
    parseVersion(null);
}, /version is required/, "Null version should throw");

assert.throws(() => {
    parseVersion("1.2");
}, /Invalid semantic version/, "Two-part version should throw");

assert.throws(() => {
    parseVersion("v1");
}, /Invalid semantic version/, "Single number should throw");

assert.throws(() => {
    parseVersion("invalid");
}, /Invalid semantic version/, "Non-numeric version should throw");

assert.throws(() => {
    parseVersion("v1.2.x");
}, /Invalid semantic version/, "Version with 'x' should throw");

// ----------------------------------------------------------
// bumpVersion tests - patch
// ----------------------------------------------------------
console.log("Testing bumpVersion - patch...");

assert.strictEqual(
    bumpVersion("v1.2.3", "patch"),
    "v1.2.4",
    "Should bump patch version"
);

assert.strictEqual(
    bumpVersion("v1.2.9", "patch"),
    "v1.2.10",
    "Should bump patch from 9 to 10"
);

assert.strictEqual(
    bumpVersion("v0.0.0", "patch"),
    "v0.0.1",
    "Should bump patch from zero"
);

assert.strictEqual(
    bumpVersion("v1.2.3-rc.1", "patch"),
    "v1.2.4",
    "Should bump patch and remove prerelease"
);

assert.strictEqual(
    bumpVersion("1.2.3", "patch"),
    "v1.2.4",
    "Should add 'v' prefix if missing"
);

// ----------------------------------------------------------
// bumpVersion tests - minor
// ----------------------------------------------------------
console.log("Testing bumpVersion - minor...");

assert.strictEqual(
    bumpVersion("v1.2.3", "minor"),
    "v1.3.0",
    "Should bump minor and reset patch"
);

assert.strictEqual(
    bumpVersion("v1.9.5", "minor"),
    "v1.10.0",
    "Should bump minor from 9 to 10"
);

assert.strictEqual(
    bumpVersion("v0.0.0", "minor"),
    "v0.1.0",
    "Should bump minor from zero"
);

assert.strictEqual(
    bumpVersion("v1.2.3-beta.1", "minor"),
    "v1.3.0",
    "Should bump minor and remove prerelease"
);

// ----------------------------------------------------------
// bumpVersion tests - major
// ----------------------------------------------------------
console.log("Testing bumpVersion - major...");

assert.strictEqual(
    bumpVersion("v1.2.3", "major"),
    "v2.0.0",
    "Should bump major and reset minor and patch"
);

assert.strictEqual(
    bumpVersion("v9.5.10", "major"),
    "v10.0.0",
    "Should bump major from 9 to 10"
);

assert.strictEqual(
    bumpVersion("v0.0.0", "major"),
    "v1.0.0",
    "Should bump major from zero"
);

assert.strictEqual(
    bumpVersion("v1.2.3-alpha.1", "major"),
    "v2.0.0",
    "Should bump major and remove prerelease"
);

// ----------------------------------------------------------
// bumpVersion error cases
// ----------------------------------------------------------
console.log("Testing bumpVersion error handling...");

assert.throws(() => {
    bumpVersion("", "patch");
}, /version is required/, "Empty version should throw");

assert.throws(() => {
    bumpVersion(null, "patch");
}, /version is required/, "Null version should throw");

assert.throws(() => {
    bumpVersion("v1.2.3", "");
}, /Invalid bump type/, "Empty bump type should throw");

assert.throws(() => {
    bumpVersion("v1.2.3", null);
}, /Invalid bump type/, "Null bump type should throw");

assert.throws(() => {
    bumpVersion("v1.2.3", "invalid");
}, /Invalid bump type/, "Invalid bump type should throw");

assert.throws(() => {
    bumpVersion("invalid", "patch");
}, /Invalid semantic version/, "Invalid version format should throw");

// ----------------------------------------------------------
// Edge cases
// ----------------------------------------------------------
console.log("Testing edge cases...");

assert.strictEqual(
    bumpVersion("v99.99.99", "patch"),
    "v99.99.100",
    "Should handle large version numbers"
);

assert.strictEqual(
    bumpVersion("v0.1.0", "minor"),
    "v0.2.0",
    "Should bump minor in 0.x.x versions"
);

assert.strictEqual(
    bumpVersion("v0.0.1", "major"),
    "v1.0.0",
    "Should graduate from 0.0.x to 1.0.0"
);

// ----------------------------------------------------------
// Consistency tests
// ----------------------------------------------------------
console.log("Testing consistency...");

const version = "v1.2.3";
const bump1 = bumpVersion(version, "patch");
const bump2 = bumpVersion(version, "patch");
assert.strictEqual(bump1, bump2, "Same inputs should produce same output");

// Sequential bumps
const v1 = "v1.0.0";
const v2 = bumpVersion(v1, "patch"); // v1.0.1
const v3 = bumpVersion(v2, "patch"); // v1.0.2
const v4 = bumpVersion(v3, "minor"); // v1.1.0
assert.strictEqual(v2, "v1.0.1", "First patch bump");
assert.strictEqual(v3, "v1.0.2", "Second patch bump");
assert.strictEqual(v4, "v1.1.0", "Minor bump resets patch");

// Verify major bump resets everything
assert.strictEqual(
    bumpVersion("v5.10.20", "major"),
    "v6.0.0",
    "Major bump should reset minor and patch"
);

console.log("✅ All tests passed.");
