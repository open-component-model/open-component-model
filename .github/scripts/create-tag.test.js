import assert from "assert";
import fs from "fs";
import path from "path";
import os from "os";
import {
  tagExists,
  resolveTagCommit,
  createAndPushTag,
  createRcTag,
  createFinalTag,
} from "./create-tag.js";

// ----------------------------------------------------------
// Helpers
// ----------------------------------------------------------

/** Create a mock execGit that returns predefined results per command pattern. */
function mockExecGit(responses = {}) {
  const calls = [];
  const fn = (args) => {
    calls.push(args);
    const key = args.join(" ");
    for (const [pattern, result] of Object.entries(responses)) {
      if (key.includes(pattern)) {
        if (result instanceof Error) throw result;
        return result;
      }
    }
    return "";
  };
  fn.calls = calls;
  return fn;
}

function mockCore() {
  const state = { failed: null, outputs: {}, logs: [] };
  return {
    setFailed: (msg) => { state.failed = msg; },
    setOutput: (k, v) => { state.outputs[k] = v; },
    info: (msg) => { state.logs.push(msg); },
    _state: state,
  };
}

function tmpDir(files = {}) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "ct-test-"));
  for (const [name, content] of Object.entries(files)) {
    fs.writeFileSync(path.join(dir, name), content);
  }
  return dir;
}

// ----------------------------------------------------------
// tagExists tests
// ----------------------------------------------------------

// Returns true when tag exists
{
  const git = mockExecGit({ "refs/tags/v1.0.0": "abc123" });
  assert.strictEqual(tagExists("v1.0.0", git), true);
}

// Returns false when tag does not exist
{
  const git = mockExecGit({ "refs/tags/v1.0.0": new Error("not found") });
  assert.strictEqual(tagExists("v1.0.0", git), false);
}

// ----------------------------------------------------------
// resolveTagCommit tests
// ----------------------------------------------------------

// Resolves commit SHA
{
  const git = mockExecGit({ "v1.0.0^{commit}": "abc123def" });
  assert.strictEqual(resolveTagCommit("v1.0.0", git), "abc123def");
}

// Throws when tag cannot be resolved
{
  const git = mockExecGit({ "v1.0.0^{commit}": new Error("not found") });
  assert.throws(() => resolveTagCommit("v1.0.0", git), /not found/);
}

// Throws when resolved SHA is empty
{
  const git = mockExecGit({ "v1.0.0^{commit}": "" });
  assert.throws(() => resolveTagCommit("v1.0.0", git), /Could not resolve/);
}

// ----------------------------------------------------------
// createAndPushTag tests
// ----------------------------------------------------------

// Creates tag at HEAD when commit is "HEAD"
{
  const git = mockExecGit({});
  createAndPushTag({ tag: "v1.0.0", commit: "HEAD", message: "release", execGit: git });
  assert.deepStrictEqual(git.calls[0], ["tag", "-a", "v1.0.0", "-m", "release"]);
  assert.deepStrictEqual(git.calls[1], ["push", "origin", "refs/tags/v1.0.0"]);
}

// Creates tag at specific commit
{
  const git = mockExecGit({});
  createAndPushTag({ tag: "v1.0.0", commit: "abc123", message: "release", execGit: git });
  assert.deepStrictEqual(git.calls[0], ["tag", "-a", "v1.0.0", "abc123", "-m", "release"]);
  assert.deepStrictEqual(git.calls[1], ["push", "origin", "refs/tags/v1.0.0"]);
}

// ----------------------------------------------------------
// createRcTag tests
// ----------------------------------------------------------

// Missing env vars → setFailed
{
  const core = mockCore();
  const origEnv = { ...process.env };
  delete process.env.TAG;
  delete process.env.CHANGELOG_FILE;
  await createRcTag({ core });
  assert.ok(core._state.failed?.includes("Missing"), `Expected setFailed, got: ${core._state.failed}`);
  process.env = origEnv;
}

// Tag already exists → idempotent skip with pushed=true
{
  const core = mockCore();
  const origEnv = { ...process.env };
  process.env.TAG = "controller/v0.1.0-rc.1";
  process.env.CHANGELOG_FILE = "/tmp/dummy.md";
  const git = mockExecGit({ "refs/tags/controller/v0.1.0-rc.1": "abc123" });
  await createRcTag({ core, execGit: git });
  assert.strictEqual(core._state.failed, null);
  assert.strictEqual(core._state.outputs.pushed, "true");
  assert.ok(core._state.logs.some((l) => l.includes("already exists")));
  process.env = origEnv;
}

// Tag does not exist → creates and pushes
{
  const core = mockCore();
  const dir = tmpDir({ "CHANGELOG.md": "## v0.1.0-rc.1\n\n- Initial release" });
  const origEnv = { ...process.env };
  process.env.TAG = "controller/v0.1.0-rc.1";
  process.env.CHANGELOG_FILE = path.join(dir, "CHANGELOG.md");
  const git = mockExecGit({
    "rev-parse refs/tags/controller/v0.1.0-rc.1": new Error("not found"),
  });
  await createRcTag({ core, execGit: git });
  assert.strictEqual(core._state.failed, null);
  assert.strictEqual(core._state.outputs.pushed, "true");
  assert.ok(core._state.logs.some((l) => l.includes("✅ Created RC tag")));
  // Verify tag and push commands were called
  const tagCall = git.calls.find((c) => c[0] === "tag");
  assert.ok(tagCall, "Expected a git tag command");
  const pushCall = git.calls.find((c) => c[0] === "push");
  assert.ok(pushCall, "Expected a git push command");
  process.env = origEnv;
}

// ----------------------------------------------------------
// createFinalTag tests
// ----------------------------------------------------------

// Missing env vars → setFailed
{
  const core = mockCore();
  const origEnv = { ...process.env };
  delete process.env.RC_TAG;
  delete process.env.FINAL_TAG;
  await createFinalTag({ core });
  assert.ok(core._state.failed?.includes("Missing"));
  process.env = origEnv;
}

// RC tag cannot be resolved → setFailed
{
  const core = mockCore();
  const origEnv = { ...process.env };
  process.env.RC_TAG = "controller/v0.1.0-rc.1";
  process.env.FINAL_TAG = "controller/v0.1.0";
  const git = mockExecGit({
    "rc.1^{commit}": new Error("not found"),
  });
  await createFinalTag({ core, execGit: git });
  assert.ok(core._state.failed !== null, "Expected setFailed on unresolvable RC tag");
  process.env = origEnv;
}

// Final tag exists at correct commit → idempotent success
{
  const core = mockCore();
  const origEnv = { ...process.env };
  process.env.RC_TAG = "controller/v0.1.0-rc.1";
  process.env.FINAL_TAG = "controller/v0.1.0";
  const git = mockExecGit({
    "rc.1^{commit}": "abc1234567890",
    "v0.1.0^{commit}": "abc1234567890",
    "refs/tags/controller/v0.1.0": "something", // tagExists check
  });
  await createFinalTag({ core, execGit: git });
  assert.strictEqual(core._state.failed, null);
  assert.ok(core._state.logs.some((l) => l.includes("idempotent rerun")));
  process.env = origEnv;
}

// Final tag exists at wrong commit → setFailed
{
  const core = mockCore();
  const origEnv = { ...process.env };
  process.env.RC_TAG = "controller/v0.1.0-rc.1";
  process.env.FINAL_TAG = "controller/v0.1.0";
  const git = mockExecGit({
    "rc.1^{commit}": "abc1234567890",
    "v0.1.0^{commit}": "def9876543210",
    "refs/tags/controller/v0.1.0": "something", // tagExists check
  });
  await createFinalTag({ core, execGit: git });
  assert.ok(core._state.failed?.includes("already exists but points to"));
  process.env = origEnv;
}

// Final tag does not exist → creates and pushes
{
  const core = mockCore();
  const origEnv = { ...process.env };
  process.env.RC_TAG = "controller/v0.1.0-rc.1";
  process.env.FINAL_TAG = "controller/v0.1.0";
  // Use a custom mock that only throws for the tagExists rev-parse check
  const calls = [];
  const git = (args) => {
    calls.push(args);
    const key = args.join(" ");
    // resolveTagCommit for RC tag
    if (key.includes("rc.1^{commit}")) return "abc1234567890";
    // tagExists check for final tag (rev-parse without ^{commit})
    if (key === "rev-parse refs/tags/controller/v0.1.0") throw new Error("not found");
    // All other commands (tag, push) succeed
    return "";
  };
  git.calls = calls;
  await createFinalTag({ core, execGit: git });
  assert.strictEqual(core._state.failed, null);
  assert.ok(core._state.logs.some((l) => l.includes("✅ Created final tag")));
  const tagCall = calls.find((c) => c[0] === "tag");
  assert.ok(tagCall, "Expected a git tag command");
  assert.ok(tagCall.includes("abc1234567890"), "Tag should be created at RC commit");
  const pushCall = calls.find((c) => c[0] === "push");
  assert.ok(pushCall, "Expected a git push command");
  process.env = origEnv;
}

console.log("✅ All create-tag tests passed.");
