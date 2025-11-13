import assert from "assert";
import { computeVersion } from "./compute-version.js";

// ----------------------------------------------------------
// CLI version tests
// ----------------------------------------------------------
console.log("Testing CLI version computation...");

assert.strictEqual(
    computeVersion("cli/v1.2.3", "cli/v"),
    "1.2.3",
    "CLI tag should extract version"
);

assert.strictEqual(
    computeVersion("cli/v2.0.0-rc.1", "cli/v"),
    "2.0.0-rc.1",
    "CLI RC tag should extract version with suffix"
);

assert.strictEqual(
    computeVersion("cli/v0.1.0", "cli/v"),
    "0.1.0",
    "CLI tag with minor version"
);

assert.strictEqual(
    computeVersion("main", "cli/v"),
    "0.0.0-main",
    "CLI main branch should create pseudo version"
);

assert.strictEqual(
    computeVersion("releases/v0.1", "cli/v"),
    "0.0.0-releases-v0.1",
    "CLI release branch should create pseudo version"
);

assert.strictEqual(
    computeVersion("feature/my-branch", "cli/v"),
    "0.0.0-feature-my-branch",
    "CLI feature branch should sanitize slashes"
);

// ----------------------------------------------------------
// Helm plugin (bindings) version tests
// ----------------------------------------------------------
console.log("Testing Helm plugin version computation...");

assert.strictEqual(
    computeVersion("bindings/go/helm/v1.2.3", "bindings/go/helm/v"),
    "1.2.3",
    "Helm plugin tag should extract version"
);

assert.strictEqual(
    computeVersion("bindings/go/helm/v2.0.0-alpha1", "bindings/go/helm/v"),
    "2.0.0-alpha1",
    "Helm plugin tag with suffix should extract version"
);

assert.strictEqual(
    computeVersion("main", "bindings/go/helm/v"),
    "0.0.0-main",
    "Helm plugin main branch should create pseudo version"
);

assert.strictEqual(
    computeVersion("feat/new-feature", "bindings/go/helm/v"),
    "0.0.0-feat-new-feature",
    "Helm plugin feature branch should sanitize slashes"
);

// ----------------------------------------------------------
// Edge cases
// ----------------------------------------------------------
console.log("Testing edge cases...");

assert.strictEqual(
    computeVersion("cli/v10.20.30", "cli/v"),
    "10.20.30",
    "Large version numbers should work"
);

assert.strictEqual(
    computeVersion("cli/v1.2", "cli/v"),
    "1.2",
    "Two-part version (major.minor) should work"
);

assert.strictEqual(
    computeVersion("cli/v1.2.3-beta.1+build.123", "cli/v"),
    "1.2.3-beta.1+build.123",
    "Complex semver with build metadata should work"
);

assert.strictEqual(
    computeVersion("pr/123/merge", "cli/v"),
    "0.0.0-pr-123-merge",
    "PR refs should be sanitized"
);

assert.strictEqual(
    computeVersion("refs/heads/main", "cli/v"),
    "0.0.0-refs-heads-main",
    "Full ref paths should be sanitized"
);

// Special characters in branch names
assert.strictEqual(
    computeVersion("feature/issue#123", "cli/v"),
    "0.0.0-feature-issue-123",
    "Branch with # should be preserved"
);

assert.strictEqual(
    computeVersion("hotfix/v1.2.3-fix", "cli/v"),
    "0.0.0-hotfix-v1.2.3-fix",
    "Branch that looks like version should not be treated as tag"
);

// ----------------------------------------------------------
// Tag prefix edge cases
// ----------------------------------------------------------
console.log("Testing tag prefix variations...");

assert.strictEqual(
    computeVersion("v1.2.3", "v"),
    "1.2.3",
    "Simple 'v' prefix should work"
);

assert.strictEqual(
    computeVersion("component/v1.2.3", "component/v"),
    "1.2.3",
    "Custom component prefix should work"
);

assert.strictEqual(
    computeVersion("deeply/nested/path/v1.2.3", "deeply/nested/path/v"),
    "1.2.3",
    "Deeply nested prefix should work"
);

// ----------------------------------------------------------
// Error cases
// ----------------------------------------------------------
console.log("Testing error handling...");

assert.throws(() => {
    computeVersion("", "cli/v");
}, /ref is required/, "Empty ref should throw");

assert.throws(() => {
    computeVersion(null, "cli/v");
}, /ref is required/, "Null ref should throw");

assert.throws(() => {
    computeVersion("main", "");
}, /tagPrefix is required/, "Empty tagPrefix should throw");

assert.throws(() => {
    computeVersion("main", null);
}, /tagPrefix is required/, "Null tagPrefix should throw");

// ----------------------------------------------------------
// Regex escaping tests (security)
// ----------------------------------------------------------
console.log("Testing regex escaping for security...");

// These should NOT match as tags even though they contain regex special chars
assert.strictEqual(
    computeVersion("cli.*v1.2.3", "cli/v"),
    "0.0.0-cli.*v1.2.3",
    "Ref with regex chars should not match tag pattern"
);

assert.strictEqual(
    computeVersion("v1.2.3", "cli/v"),
    "0.0.0-v1.2.3",
    "Ref without correct prefix should not match"
);

// ----------------------------------------------------------
// Consistency tests
// ----------------------------------------------------------
console.log("Testing consistency...");

// Same input should always produce same output
const ref1 = "cli/v1.2.3";
const prefix1 = "cli/v";
const result1a = computeVersion(ref1, prefix1);
const result1b = computeVersion(ref1, prefix1);
assert.strictEqual(result1a, result1b, "Same inputs should produce same output");

// Different prefixes should not interfere
assert.notStrictEqual(
    computeVersion("cli/v1.2.3", "cli/v"),
    computeVersion("cli/v1.2.3", "bindings/go/helm/v"),
    "Different prefixes should produce different results"
);

console.log("✅ All tests passed.");
