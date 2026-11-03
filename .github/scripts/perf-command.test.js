// @ts-check
import assert from "assert";
import { test } from "node:test";
import { parsePerfCommand } from "./perf-command.js";

test("bare /perf uses every default", () => {
  assert.deepStrictEqual(parsePerfCommand("/perf"), {});
});

test("flags are read from the first line only", () => {
  assert.deepStrictEqual(
    parsePerfCommand("/perf --scenario=complex --objects=1000 --repeats=3\r\nthanks!"),
    { scenarios: "complex", objects: "1000", repeats: "3" },
  );
});

test("other comments are not commands", () => {
  assert.strictEqual(parsePerfCommand("/performance is great"), null);
  assert.strictEqual(parsePerfCommand("lgtm"), null);
});

test("unknown or empty flags are rejected", () => {
  assert.throws(() => parsePerfCommand("/perf --foo=bar"), /invalid argument: --foo=bar/);
  assert.throws(() => parsePerfCommand("/perf --repeats="), /invalid argument: --repeats=/);
  assert.throws(() => parsePerfCommand("/perf --constructor=x"), /invalid argument/);
});
