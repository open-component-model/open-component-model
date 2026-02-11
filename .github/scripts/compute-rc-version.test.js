import assert from "assert";
import { computeNextVersions, isStableNewer, parseBranch, parseVersion } from "./compute-rc-version.js";

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

// 2. New RC from existing RC (RC increments)
const v2 = computeNextVersions("0.1", "cli/v0.1.1", "cli/v0.1.1-rc.4", false);
assert.deepStrictEqual(v2, {
    baseVersion: "0.1.2",
    rcVersion: "0.1.2-rc.1",
}, "RC version should be incremented when starting from an existing RC");

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

console.log("✅ All tests passed.");
