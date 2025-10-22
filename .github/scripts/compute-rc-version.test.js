import assert from "assert";
import {computeNextVersions, isStableNewer, parseBranch, parseVersion,} from "./compute-rc-version.js";

const noopRun = () => "";

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
const v1 = computeNextVersions("0.1", "cli/v0.1.0", "", noopRun);
assert.deepStrictEqual(v1, {
    baseVersion: "0.1.1",
    rcVersion: "0.1.1-rc.1",
});

// 2. New RC from existing RC (RC increments)
const v2 = computeNextVersions("0.1", "cli/v0.1.1", "cli/v0.1.1-rc.4", noopRun);
assert.deepStrictEqual(v2, {
    baseVersion: "0.1.1",
    rcVersion: "0.1.1-rc.5",
});

// 3. No stable tag (starting fresh)
const v3 = computeNextVersions("0.2", "", "", noopRun);
assert.deepStrictEqual(v3, {
    baseVersion: "0.2.0",
    rcVersion: "0.2.0-rc.1",
});

// 4. Stable newer than RC (patch bump)
const v4 = computeNextVersions("0.1", "cli/v0.1.2", "cli/v0.1.1-rc.7", noopRun);
assert.deepStrictEqual(v4, {
    baseVersion: "0.1.3",
    rcVersion: "0.1.3-rc.1",
});

// 5. RC newer than stable (RC increment)
const v5 = computeNextVersions("0.1", "cli/v0.1.2", "cli/v0.1.3-rc.9", noopRun);
assert.deepStrictEqual(v5, {
    baseVersion: "0.1.2",
    rcVersion: "0.1.2-rc.10",
});

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
