import assert from "assert";
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

/** Run fn with temporary env vars, restoring originals via try/finally. */
async function withEnv(vars, fn) {
  const saved = {};
  for (const key of Object.keys(vars)) {
    saved[key] = process.env[key];
    if (vars[key] === undefined) delete process.env[key];
    else process.env[key] = vars[key];
  }
  try {
    await fn();
  } finally {
    for (const key of Object.keys(saved)) {
      if (saved[key] === undefined) delete process.env[key];
      else process.env[key] = saved[key];
    }
  }
}

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
  await withEnv({ TAG: undefined }, async () => {
    await createRcTag({ core });
    assert.ok(core._state.failed?.includes("Missing"), `Expected setFailed, got: ${core._state.failed}`);
  });
}

// Tag already exists at HEAD → idempotent skip with pushed=true
{
  const core = mockCore();
  await withEnv({ TAG: "controller/v0.1.0-rc.1" }, async () => {
    const git = mockExecGit({
      "refs/tags/controller/v0.1.0-rc.1": "abc123",
      "rc.1^{commit}": "abc123",
      "rev-parse HEAD": "abc123",
    });
    await createRcTag({ core, execGit: git });
    assert.strictEqual(core._state.failed, null);
    assert.strictEqual(core._state.outputs.pushed, "true");
    assert.ok(core._state.logs.some((l) => l.includes("already exists")));
  });
}

// Tag does not exist → creates and pushes
{
  const core = mockCore();
  await withEnv({ TAG: "controller/v0.1.0-rc.1" }, async () => {
    const git = mockExecGit({
      "rev-parse refs/tags/controller/v0.1.0-rc.1": new Error("not found"),
    });
    await createRcTag({ core, execGit: git });
    assert.strictEqual(core._state.failed, null);
    assert.strictEqual(core._state.outputs.pushed, "true");
    assert.ok(core._state.logs.some((l) => l.includes("✅ Created RC tag")));
    const tagCall = git.calls.find((c) => c[0] === "tag");
    assert.ok(tagCall, "Expected a git tag command");
    assert.ok(tagCall.includes("Release candidate controller/v0.1.0-rc.1"), "Expected simple RC message as tag annotation");
    const pushCall = git.calls.find((c) => c[0] === "push");
    assert.ok(pushCall, "Expected a git push command");
  });
}

// ----------------------------------------------------------
// createFinalTag tests
// ----------------------------------------------------------

// Missing env vars → setFailed
{
  const core = mockCore();
  await withEnv({ RC_TAG: undefined, FINAL_TAG: undefined }, async () => {
    await createFinalTag({ core });
    assert.ok(core._state.failed?.includes("Missing"));
  });
}

// RC tag cannot be resolved → setFailed
{
  const core = mockCore();
  await withEnv({ RC_TAG: "controller/v0.1.0-rc.1", FINAL_TAG: "controller/v0.1.0" }, async () => {
    const git = mockExecGit({
      "rc.1^{commit}": new Error("not found"),
    });
    await createFinalTag({ core, execGit: git });
    assert.ok(core._state.failed !== null, "Expected setFailed on unresolvable RC tag");
  });
}

// Final tag exists at correct commit → idempotent success
{
  const core = mockCore();
  await withEnv({ RC_TAG: "controller/v0.1.0-rc.1", FINAL_TAG: "controller/v0.1.0" }, async () => {
    const git = mockExecGit({
      "rc.1^{commit}": "abc1234567890",
      "v0.1.0^{commit}": "abc1234567890",
      "refs/tags/controller/v0.1.0": "something", // tagExists check
    });
    await createFinalTag({ core, execGit: git });
    assert.strictEqual(core._state.failed, null);
    assert.ok(core._state.logs.some((l) => l.includes("idempotent rerun")));
  });
}

// Final tag exists at wrong commit → setFailed
{
  const core = mockCore();
  await withEnv({ RC_TAG: "controller/v0.1.0-rc.1", FINAL_TAG: "controller/v0.1.0" }, async () => {
    const git = mockExecGit({
      "rc.1^{commit}": "abc1234567890",
      "v0.1.0^{commit}": "def9876543210",
      "refs/tags/controller/v0.1.0": "something", // tagExists check
    });
    await createFinalTag({ core, execGit: git });
    assert.ok(core._state.failed?.includes("already exists but points to"));
  });
}

// Final tag does not exist → creates and pushes
{
  const core = mockCore();
  await withEnv({ RC_TAG: "controller/v0.1.0-rc.1", FINAL_TAG: "controller/v0.1.0" }, async () => {
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
  });
}

console.log("✅ All create-tag tests passed.");
