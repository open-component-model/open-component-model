import assert from "assert";
import {
    computeNextVersions,
    isStableNewer,
    parseBranch,
    parseVersion,
    extractHighestPreviousReleaseVersion,
    shouldSetLatest,
    findPreviousTag,
} from "./release-versioning.js";

// ----------------------------------------------------------
// parseVersion tests
// ----------------------------------------------------------
assert.deepStrictEqual(parseVersion("cli/v0.1.2"), [0, 1, 2]);
assert.deepStrictEqual(parseVersion("cli/v0.1.2-rc.3"), [0, 1, 2]);
assert.deepStrictEqual(parseVersion("v1.0.0"), [1, 0, 0]);
assert.deepStrictEqual(parseVersion("v1.0.0-rc.99"), [1, 0, 0]);
assert.deepStrictEqual(parseVersion("some/other/prefix/v2.3.4"), [2, 3, 4]);
assert.deepStrictEqual(parseVersion(""), []);

// ----------------------------------------------------------
// parseBranch tests
// ----------------------------------------------------------
assert.strictEqual(parseBranch("releases/v0.1"), "0.1");
assert.strictEqual(parseBranch("releases/v0.100"), "0.100");
assert.throws(() => parseBranch("release/v0.1"), /Invalid branch/);
assert.throws(() => parseBranch("v0.1"), /Invalid branch/);
assert.throws(() => parseBranch("releases/1.0"), /Invalid branch/);

// ----------------------------------------------------------
// computeNextVersions tests
// ----------------------------------------------------------

// 1. New RC from a stable version (patch bumps)
const v1 = computeNextVersions("0.1", "cli/v0.1.0", "", false);
assert.deepStrictEqual(v1, {
    baseVersion: "0.1.1",
    rcVersion: "0.1.1-rc.1",
}, "RC version should be bumped when starting from a stable version");

// 2. Stable + RC on same base => start next minor RC line
const v2 = computeNextVersions("0.1", "cli/v0.1.1", "cli/v0.1.1-rc.4", false);
assert.deepStrictEqual(v2, {
    baseVersion: "0.1.2",
    rcVersion: "0.1.2-rc.1",
}, "When stable exists for the same base as latest RC, start next minor RC line");

// 2b. No stable yet for current line + existing RC => continue same RC line
const v2b = computeNextVersions("0.4", "", "cli/v0.4.0-rc.3", false);
assert.deepStrictEqual(v2b, {
    baseVersion: "0.4.0",
    rcVersion: "0.4.0-rc.4",
}, "Without a stable tag, RC line must continue on the same base");

// 3. Same base between stable and RC with minor version bump
const v3 = computeNextVersions("0.1", "cli/v0.1.0", "cli/v0.1.0-rc.4", true);
assert.deepStrictEqual(v3, {
    baseVersion: "0.2.0",
    rcVersion: "0.2.0-rc.1",
}, "Base version should be bumped and RC version should again start from 1");

// 4. No stable tag (starting fresh)
const v4 = computeNextVersions("0.2", "", "");
assert.deepStrictEqual(v4, {
    baseVersion: "0.2.0",
    rcVersion: "0.2.0-rc.1",
}, "RC version should be bumped and base version should start with 0 when starting without a tag");

// 5. Stable newer than RC (patch bump)
const v5 = computeNextVersions("0.1", "cli/v0.1.2", "cli/v0.1.1-rc.7", false);
assert.deepStrictEqual(v5, {
    baseVersion: "0.1.3",
    rcVersion: "0.1.3-rc.1",
}, "latest stable should bump patch and start new RC sequence");

// 6. RC newer than stable (RC increment with new base version)
const v6 = computeNextVersions("0.1", "cli/v0.1.2", "cli/v0.1.3-rc.9", false);
assert.deepStrictEqual(v6, {
    baseVersion: "0.1.3",
    rcVersion: "0.1.3-rc.10",
}, "RC should be incremented and base version should be bumped when last RC is newer than last stable");

// 7. Malformed tag causes bump
const v7 = computeNextVersions("0.1", "cli/v0.1.1", "cli/v0.1.1-rc.", false);
assert.deepStrictEqual(v7, {
    baseVersion: "0.1.2",
    rcVersion: "0.1.2-rc.1",
}, "Should default to bump when malformed tag is discovered");

// 8. New Version bump without RC version.
const v8 = computeNextVersions("0.1.1", "v0.1.1", "", false);
assert.deepStrictEqual(v8, {
    baseVersion: "0.1.2", // this should be increased
    rcVersion: "0.1.2-rc.1",
}, "Base version should be increased.");


// 9. New version minor bump.
const v9 = computeNextVersions("0.2.2", "v0.2.2", "", true);
assert.deepStrictEqual(v9, {
    baseVersion: "0.3.0",
    rcVersion: "0.3.0-rc.1",
}, "Base version should be increased.");


// ----------------------------------------------------------
// isStableNewer tests
// ----------------------------------------------------------
assert.ok(
    isStableNewer("cli/v0.1.2", "cli/v0.1.1-rc.5"),
    "Stable should win when newer than RC"
);

assert.ok(
    !isStableNewer("cli/v0.1.2", "cli/v0.1.3-rc.5"),
    "RC should win when newer than stable"
);

assert.ok(
    isStableNewer("cli/v0.1.2", ""),
    "Stable should win if no RC exists"
);

assert.ok(
    !isStableNewer("", "cli/v0.1.2-rc.4"),
    "Should return false if no stable tag"
);

// ----------------------------------------------------------
// extractHighestPreviousReleaseVersion tests
// ----------------------------------------------------------
const mockReleases = [
    { prerelease: false, tag_name: "v0.1.0" },
    { prerelease: true, tag_name: "v0.1.1-rc.1" },
    { prerelease: false, tag_name: "v0.1.2" },
    { prerelease: false, tag_name: "v0.2.0" },
    { prerelease: false, tag_name: "kubernetes/controller/v1.0.0" }, // side tag, ignored
    { prerelease: true, tag_name: "v0.3.0-rc.1" },
];

assert.strictEqual(
    extractHighestPreviousReleaseVersion(mockReleases),
    "0.2.0",
    "Should return highest non-prerelease canonical version"
);

assert.strictEqual(
    extractHighestPreviousReleaseVersion([]),
    "",
    "Should return empty string for no releases"
);

assert.strictEqual(
    extractHighestPreviousReleaseVersion([{ prerelease: true, tag_name: "v0.1.0-rc.1" }]),
    "",
    "Should return empty string if only prereleases exist"
);

assert.strictEqual(
    extractHighestPreviousReleaseVersion([{ prerelease: false, tag_name: "kubernetes/controller/v0.5.0" }]),
    "",
    "Should ignore prefixed Go-module side tags"
);

// Legacy fallback: when no canonical v* tags exist, fall back to cli/v* tags.
// TODO(unified-release-cutover): drop these tests when the fallback is removed.
assert.strictEqual(
    extractHighestPreviousReleaseVersion([
        { prerelease: false, tag_name: "cli/v0.6.1" },
        { prerelease: false, tag_name: "cli/v0.6.2" },
        { prerelease: true, tag_name: "cli/v0.7.0-rc.1" },
    ]),
    "0.6.2",
    "Should fall back to legacy cli/v* tags when no canonical v* exist"
);

// Canonical takes precedence over legacy when both exist.
assert.strictEqual(
    extractHighestPreviousReleaseVersion([
        { prerelease: false, tag_name: "cli/v0.6.2" },
        { prerelease: false, tag_name: "v0.7.0" },
    ]),
    "0.7.0",
    "Canonical v* should take precedence over legacy cli/v*"
);

// ----------------------------------------------------------
// shouldSetLatest tests
// ----------------------------------------------------------
assert.ok(
    shouldSetLatest("0.2.0", ""),
    "Should return true if no existing previous release version"
);

assert.ok(
    shouldSetLatest("0.2.0", "0.1.0"),
    "Should return true if promotion > highest"
);

assert.ok(
    shouldSetLatest("0.2.0", "0.2.0"),
    "Should return true if promotion == highest"
);

assert.ok(
    !shouldSetLatest("0.1.0", "0.2.0"),
    "Should return false if promotion < highest"
);

assert.ok(
    shouldSetLatest("0.10.0", "0.9.0"),
    "Should handle numeric comparison correctly (0.10 > 0.9)"
);

// ----------------------------------------------------------
// findPreviousTag tests
// ----------------------------------------------------------

// Normal case: finds the latest non-RC canonical tag, excluding the new tag
assert.strictEqual(
    findPreviousTag(
        ["v0.1.0", "v0.1.0-rc.1", "v0.2.0-rc.1"],
        "v0.2.0-rc.1"
    ),
    "v0.1.0",
    "Should find v0.1.0 as previous tag"
);

// First release: no previous non-RC tags exist
assert.strictEqual(
    findPreviousTag(["v0.1.0-rc.1"], "v0.1.0-rc.1"),
    "",
    "Should return empty string when only the new RC tag exists"
);

// Multiple stable versions: picks the highest
assert.strictEqual(
    findPreviousTag(
        ["v0.1.0", "v0.1.1", "v0.2.0-rc.1"],
        "v0.2.0-rc.1"
    ),
    "v0.1.1",
    "Should return the highest non-RC tag"
);

// Excludes the new tag even if it's a stable tag
assert.strictEqual(
    findPreviousTag(["v0.1.0", "v0.2.0"], "v0.2.0"),
    "v0.1.0",
    "Should exclude the new tag itself from results"
);

// Empty input
assert.strictEqual(
    findPreviousTag([], "v0.1.0-rc.1"),
    "",
    "Should return empty string for empty tag list"
);

// Only RC tags (first release scenario)
assert.strictEqual(
    findPreviousTag(
        ["v0.1.0-rc.1", "v0.1.0-rc.2"],
        "v0.1.0-rc.3"
    ),
    "",
    "Should return empty string when only RC tags exist"
);

// Side tags (kubernetes/controller/*) are ignored when finding previous canonical
assert.strictEqual(
    findPreviousTag(
        ["v0.1.0", "kubernetes/controller/v0.1.0", "v0.2.0-rc.1"],
        "v0.2.0-rc.1"
    ),
    "v0.1.0",
    "Should ignore prefixed Go-module side tags"
);

// Version sorting: 0.10.0 > 0.9.0 > 0.2.0
assert.strictEqual(
    findPreviousTag(
        ["v0.2.0", "v0.9.0", "v0.10.0", "v0.11.0-rc.1"],
        "v0.11.0-rc.1"
    ),
    "v0.10.0",
    "Should sort versions numerically, not lexicographically"
);

// Ignore non-semver tags
assert.strictEqual(
    findPreviousTag(
        ["v2-experimental", "v0.2.0-rc.1"],
        "v0.2.0-rc.1"
    ),
    "",
    "Should ignore non-semver non-RC tags"
);

// A stable tag newer than newTag must NOT be returned
assert.strictEqual(
    findPreviousTag(
        ["v0.1.0", "v0.3.0", "v0.2.0-rc.1"],
        "v0.2.0-rc.1"
    ),
    "v0.1.0",
    "Should not return v0.3.0 because it is newer than the new tag"
);

console.log("✅ All tests passed.");
