// @ts-check
import assert from "assert";
import { test } from "node:test";
import perfCommand, { isPerfCommand } from "./perf-command.js";

test("/perf is a command, also with more lines below", () => {
  assert.strictEqual(isPerfCommand("/perf"), true);
  assert.strictEqual(isPerfCommand("/perf\r\nthanks!"), true);
});

test("other comments are not commands", () => {
  assert.strictEqual(isPerfCommand("/performance is great"), false);
  assert.strictEqual(isPerfCommand("lgtm"), false);
  assert.strictEqual(isPerfCommand(" /perf"), false);
});

test("arguments are rejected", () => {
  assert.throws(() => isPerfCommand("/perf --objects=100"), /takes no arguments/);
});

/**
 * Fake the dependencies of perfCommand for a "/perf" comment whose author gets the given permission lookup.
 *
 * @param {() => Promise<{ data: { permission: string, role_name: string } }>} getCollaboratorPermissionLevel
 */
function fakePerf(getCollaboratorPermissionLevel) {
  const calls = { outputs: /** @type {Record<string, string>} */ ({}), reactions: /** @type {string[]} */ ([]), failed: "", pullsGet: 0 };
  const github = {
    rest: {
      repos: {
        getCollaboratorPermissionLevel: async (/** @type {object} */ params) => {
          // The check must be about the comment author, not the repository owner or anyone else.
          assert.deepStrictEqual(params, { owner: "o", repo: "r", username: "someone" });
          return getCollaboratorPermissionLevel();
        },
        compareCommitsWithBasehead: async () => ({ data: { merge_base_commit: { sha: "base" } } }),
      },
      pulls: {
        get: async () => {
          calls.pullsGet++;
          return { data: { base: { ref: "main", sha: "tip" }, head: { sha: "head" } } };
        },
      },
      reactions: { createForIssueComment: async (/** @type {{ content: string }} */ { content }) => calls.reactions.push(content) },
    },
  };
  const context = {
    repo: { owner: "o", repo: "r" },
    payload: { comment: { id: 1, body: "/perf", user: { login: "someone" } }, issue: { number: 2 } },
  };
  const core = {
    setOutput: (/** @type {string} */ name, /** @type {string} */ value) => { calls.outputs[name] = value; },
    setFailed: (/** @type {string} */ message) => { calls.failed = message; },
  };
  // @ts-ignore: only the parts perfCommand uses.
  return { calls, run: () => perfCommand({ github, context, core }) };
}

test("/perf runs for authors who can merge", async () => {
  for (const permission of ["admin", "write"]) {
    const { calls, run } = fakePerf(async () => ({ data: { permission, role_name: permission } }));
    await run();
    assert.strictEqual(calls.outputs.run, "true", permission);
    assert.strictEqual(calls.outputs.pr_sha, "head", permission);
    assert.strictEqual(calls.failed, "", permission);
  }
});

test("/perf is refused for authors who cannot merge", async () => {
  for (const [permission, role] of [["read", "triage"], ["none", "none"]]) {
    const { calls, run } = fakePerf(async () => ({ data: { permission, role_name: role } }));
    await run();
    assert.notStrictEqual(calls.outputs.run, "true", permission);
    assert.strictEqual(calls.pullsGet, 0, permission);
    assert.deepStrictEqual(calls.reactions, ["confused"], permission);
    assert.match(calls.failed, new RegExp(`someone has ${role}$`), permission);
  }
});

test("/perf does not run when the permission lookup fails", async () => {
  const { calls, run } = fakePerf(async () => {
    throw new Error("Not Found");
  });
  await assert.rejects(run, /Not Found/);
  assert.notStrictEqual(calls.outputs.run, "true");
  assert.strictEqual(calls.pullsGet, 0);
});
