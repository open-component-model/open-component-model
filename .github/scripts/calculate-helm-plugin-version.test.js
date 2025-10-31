import assert from "assert";
import {computeVersion, getLatestTag, validateSemver, validateTagExists} from "./calculate-helm-plugin-version.js";

// Mock core object for testing
const mockCore = {
    info: (msg) => {}, // Silent in tests
    warning: (msg) => {},
};

// ----------------------------------------------------------
// computeVersion tests
// ----------------------------------------------------------

// Test 1: workflow_dispatch with manual version "main"
const v1 = computeVersion(
    mockCore,
    "workflow_dispatch",
    "refs/heads/main",
    "abc123def456789012345678",
    "main"
);
assert.ok(
    v1.endsWith("-dev.abc123def456"),
    `Expected dev version with SHA, got: ${v1}`
);

// Test 2: workflow_dispatch with empty version (treated as "main")
const v2 = computeVersion(
    mockCore,
    "workflow_dispatch",
    "refs/heads/main",
    "abc123def456789012345678",
    ""
);
assert.ok(
    v2.endsWith("-dev.abc123def456"),
    `Expected dev version with SHA, got: ${v2}`
);

// Test 3: workflow_dispatch with explicit semver (without tag validation in test)
try {
    computeVersion(
        mockCore,
        "workflow_dispatch",
        "refs/heads/main",
        "abc123def456789012345678",
        "1.2.3"
    );
    // This will fail because the tag doesn't exist, which is expected
    assert.fail("Should have thrown an error for non-existent tag");
} catch (error) {
    assert.ok(
        error.message.includes("does not exist"),
        `Expected tag validation error, got: ${error.message}`
    );
}

// Test 4: push to main branch
const v4 = computeVersion(
    mockCore,
    "push",
    "refs/heads/main",
    "def456abc789012345678901",
    ""
);
assert.ok(
    v4.endsWith("-dev.def456abc789"),
    `Expected dev version with SHA, got: ${v4}`
);

// Test 5: tag push
const v5 = computeVersion(
    mockCore,
    "push",
    "refs/tags/bindings/go/helm/v1.2.3",
    "123456789012345678901234",
    ""
);
assert.strictEqual(v5, "1.2.3", `Expected tag version 1.2.3, got: ${v5}`);

// Test 6: tag push with suffix
const v6 = computeVersion(
    mockCore,
    "push",
    "refs/tags/bindings/go/helm/v2.0.0-alpha1",
    "234567890123456789012345",
    ""
);
assert.strictEqual(v6, "2.0.0-alpha1", `Expected tag version 2.0.0-alpha1, got: ${v6}`);

// Test 7: pull request
const v7 = computeVersion(
    mockCore,
    "pull_request",
    "refs/pull/1100/merge",
    "345678901234567890123456",
    ""
);
assert.ok(
    v7.match(/^.*-pr\.1100\.345678901234$/),
    `Expected PR version with number and SHA, got: ${v7}`
);

// Test 8: pull request with different number
const v8 = computeVersion(
    mockCore,
    "pull_request",
    "refs/pull/42/merge",
    "456789012345678901234567",
    ""
);
assert.ok(
    v8.match(/^.*-pr\.42\.456789012345$/),
    `Expected PR version with number and SHA, got: ${v8}`
);

// Test 9: unsupported ref should throw
assert.throws(() => {
    computeVersion(
        mockCore,
        "push",
        "refs/heads/feature/foo",
        "567890123456789012345678",
        ""
    );
}, /Unsupported ref/);

// Test 10: unsupported event with unsupported ref should throw
assert.throws(() => {
    computeVersion(
        mockCore,
        "unknown_event",
        "refs/heads/feature/unknown",
        "678901234567890123456789",
        ""
    );
}, /Unsupported ref/);

// ----------------------------------------------------------
// validateSemver tests
// ----------------------------------------------------------

// Valid semver formats
validateSemver("0.0.0");
validateSemver("1.2.3");
validateSemver("10.20.30");
validateSemver("1.0.0-alpha");
validateSemver("1.0.0-alpha.1");
validateSemver("1.0.0-0.3.7");
validateSemver("1.0.0-x.7.z.92");
validateSemver("1.0.0-rc.1");
validateSemver("2.0.0-beta.11");

// Invalid semver formats
assert.throws(() => validateSemver("1.2"), /Invalid version format/);
assert.throws(() => validateSemver("1"), /Invalid version format/);
assert.throws(() => validateSemver("v1.2.3"), /Invalid version format/);
assert.throws(() => validateSemver("1.2.3.4"), /Invalid version format/);
assert.throws(() => validateSemver("a.b.c"), /Invalid version format/);
assert.throws(() => validateSemver(""), /Invalid version format/);
assert.throws(() => validateSemver("1.2.3 "), /Invalid version format/);
assert.throws(() => validateSemver(" 1.2.3"), /Invalid version format/);
assert.throws(() => validateSemver("1.2.3-"), /Invalid version format/);
assert.throws(() => validateSemver("1.2.3-rc 1"), /Invalid version format/);

// ----------------------------------------------------------
// Determinism test
// ----------------------------------------------------------

// Test 11: Same inputs produce same output (determinism for re-runs)
const deterministicV1 = computeVersion(
    mockCore,
    "push",
    "refs/heads/main",
    "aabbccddeeff112233445566",
    ""
);
const deterministicV2 = computeVersion(
    mockCore,
    "push",
    "refs/heads/main",
    "aabbccddeeff112233445566",
    ""
);
assert.strictEqual(
    deterministicV1,
    deterministicV2,
    "Same inputs should produce same version (deterministic)"
);

// Test 12: Different SHAs produce different versions
const differentShaV1 = computeVersion(
    mockCore,
    "push",
    "refs/heads/main",
    "111111111111111111111111",
    ""
);
const differentShaV2 = computeVersion(
    mockCore,
    "push",
    "refs/heads/main",
    "222222222222222222222222",
    ""
);
assert.notStrictEqual(
    differentShaV1,
    differentShaV2,
    "Different SHAs should produce different versions"
);

// ----------------------------------------------------------
// Edge cases
// ----------------------------------------------------------

// Test 13: Very long PR number
const v13 = computeVersion(
    mockCore,
    "pull_request",
    "refs/pull/999999/merge",
    "789012345678901234567890",
    ""
);
assert.ok(
    v13.includes("-pr.999999."),
    `Expected PR version with large number, got: ${v13}`
);

// Test 14: Semver with complex suffix
validateSemver("1.0.0-alpha.beta.1");
validateSemver("1.0.0-x-y-z.123");
validateSemver("1.0.0-0.0.0");

console.log("✅ All tests passed.");
