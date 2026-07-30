import assert from "assert";
import {
  tagExists,
  resolveTagCommit,
  createAndPushTag,
  createRcTags,
  createNewReleaseTags,
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
  assert.deepStrictEqual(git.calls[0], ["tag", "-s", "v1.0.0", "-m", "release"]);
  assert.deepStrictEqual(git.calls[1], ["push", "origin", "refs/tags/v1.0.0"]);
}

// Creates tag at specific commit
{
  const git = mockExecGit({});
  createAndPushTag({ tag: "v1.0.0", commit: "abc123", message: "release", execGit: git });
  assert.deepStrictEqual(git.calls[0], ["tag", "-s", "v1.0.0", "abc123", "-m", "release"]);
  assert.deepStrictEqual(git.calls[1], ["push", "origin", "refs/tags/v1.0.0"]);
}

// ----------------------------------------------------------
// createRcTags tests
// ----------------------------------------------------------

// Missing CANONICAL_TAG → setFailed
{
  const core = mockCore();
  await withEnv({ CANONICAL_TAG: undefined, ADDITIONAL_TAGS: undefined }, async () => {
    await createRcTags({ core });
    assert.ok(core._state.failed?.includes("Missing"), `Expected setFailed, got: ${core._state.failed}`);
  });
}

// All tags created when none exist (canonical + multiple module tags)
{
  const core = mockCore();
  await withEnv({
    CANONICAL_TAG: "v0.1.0-rc.1",
    ADDITIONAL_TAGS: "cli/v0.1.0-rc.1,kubernetes/controller/v0.1.0-rc.1",
  }, async () => {
    const git = mockExecGit({
      "rev-parse refs/tags/v0.1.0-rc.1": new Error("not found"),
      "rev-parse refs/tags/cli/v0.1.0-rc.1": new Error("not found"),
      "rev-parse refs/tags/kubernetes/controller/v0.1.0-rc.1": new Error("not found"),
    });
    await createRcTags({ core, execGit: git });
    assert.strictEqual(core._state.failed, null);
    assert.strictEqual(core._state.outputs.pushed, "true");
    const tagCalls = git.calls.filter((c) => c[0] === "tag");
    assert.strictEqual(tagCalls.length, 3, "Expected three tag commands (canonical + 2 module tags)");
  });
}

// Canonical-only when ADDITIONAL_TAGS missing
{
  const core = mockCore();
  await withEnv({ CANONICAL_TAG: "v0.1.0-rc.1", ADDITIONAL_TAGS: undefined }, async () => {
    const git = mockExecGit({
      "rev-parse refs/tags/v0.1.0-rc.1": new Error("not found"),
    });
    await createRcTags({ core, execGit: git });
    assert.strictEqual(core._state.failed, null);
    assert.strictEqual(core._state.outputs.pushed, "true");
    const tagCalls = git.calls.filter((c) => c[0] === "tag");
    assert.strictEqual(tagCalls.length, 1, "Expected only canonical tag");
  });
}

// Whitespace and empty entries in ADDITIONAL_TAGS are tolerated
{
  const core = mockCore();
  await withEnv({
    CANONICAL_TAG: "v0.1.0-rc.1",
    ADDITIONAL_TAGS: " cli/v0.1.0-rc.1 ,, kubernetes/controller/v0.1.0-rc.1 ",
  }, async () => {
    const git = mockExecGit({
      "rev-parse refs/tags/v0.1.0-rc.1": new Error("not found"),
      "rev-parse refs/tags/cli/v0.1.0-rc.1": new Error("not found"),
      "rev-parse refs/tags/kubernetes/controller/v0.1.0-rc.1": new Error("not found"),
    });
    await createRcTags({ core, execGit: git });
    assert.strictEqual(core._state.failed, null);
    const tagCalls = git.calls.filter((c) => c[0] === "tag");
    assert.strictEqual(tagCalls.length, 3, "Whitespace and empty entries should be ignored");
  });
}

// Existing tag at HEAD is idempotent
{
  const core = mockCore();
  await withEnv({ CANONICAL_TAG: "v0.1.0-rc.1", ADDITIONAL_TAGS: undefined }, async () => {
    const git = mockExecGit({
      "refs/tags/v0.1.0-rc.1": "abc123",
      "rc.1^{commit}": "abc123",
      "rev-parse HEAD": "abc123",
    });
    await createRcTags({ core, execGit: git });
    assert.strictEqual(core._state.failed, null);
    assert.strictEqual(core._state.outputs.pushed, "true");
    assert.ok(core._state.logs.some((l) => l.includes("already exists")));
  });
}

// ----------------------------------------------------------
// createNewReleaseTags tests
// ----------------------------------------------------------

// Missing required env vars → setFailed
{
  const core = mockCore();
  await withEnv({ RC_TAG: undefined, NEW_RELEASE_TAG: undefined, ADDITIONAL_TAGS: undefined }, async () => {
    await createNewReleaseTags({ core });
    assert.ok(core._state.failed?.includes("Missing"));
  });
}

// RC tag cannot be resolved → setFailed
{
  const core = mockCore();
  await withEnv({ RC_TAG: "v0.1.0-rc.1", NEW_RELEASE_TAG: "v0.1.0", ADDITIONAL_TAGS: undefined }, async () => {
    const git = mockExecGit({ "rc.1^{commit}": new Error("not found") });
    await createNewReleaseTags({ core, execGit: git });
    assert.ok(core._state.failed !== null, "Expected setFailed on unresolvable RC tag");
  });
}

// All new tags created when none exist (canonical + multiple module tags)
{
  const core = mockCore();
  await withEnv({
    RC_TAG: "v0.1.0-rc.1",
    NEW_RELEASE_TAG: "v0.1.0",
    ADDITIONAL_TAGS: "cli/v0.1.0,kubernetes/controller/v0.1.0",
  }, async () => {
    const calls = [];
    const git = (args) => {
      calls.push(args);
      const key = args.join(" ");
      if (key.includes("rc.1^{commit}")) return "abc1234567890";
      if (key === "rev-parse refs/tags/v0.1.0") throw new Error("not found");
      if (key === "rev-parse refs/tags/cli/v0.1.0") throw new Error("not found");
      if (key === "rev-parse refs/tags/kubernetes/controller/v0.1.0") throw new Error("not found");
      return "";
    };
    git.calls = calls;
    await createNewReleaseTags({ core, execGit: git });
    assert.strictEqual(core._state.failed, null);
    assert.strictEqual(core._state.outputs.pushed, "true");
    const tagCalls = calls.filter((c) => c[0] === "tag");
    assert.strictEqual(tagCalls.length, 3, "Expected three tag commands (canonical + 2 module tags)");
    assert.ok(tagCalls.every((c) => c.includes("abc1234567890")), "All tags should point at RC commit");
  });
}

// Wrong-commit existing tag → setFailed
{
  const core = mockCore();
  await withEnv({
    RC_TAG: "v0.1.0-rc.1",
    NEW_RELEASE_TAG: "v0.1.0",
    ADDITIONAL_TAGS: undefined,
  }, async () => {
    const git = mockExecGit({
      "rc.1^{commit}": "abc1234567890",
      "v0.1.0^{commit}": "def9876543210",
      "refs/tags/v0.1.0": "something",
    });
    await createNewReleaseTags({ core, execGit: git });
    assert.ok(core._state.failed?.includes("exists at"));
  });
}

console.log("All create-tag tests passed.");
