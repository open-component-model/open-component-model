// @ts-check
import assert from "assert";
import { test } from "node:test";
import { isPerfCommand } from "./perf-command.js";

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
