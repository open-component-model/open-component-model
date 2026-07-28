// @ts-check
import assert from "assert";
import { test } from "node:test";
import { deriveLabels, nullProtoMap } from "./label-from-title.js";

// Mirrors the maps configured in .github/workflows/pull-request.yaml.
const maps = {
  typeToLabel: {
    feat: "kind/feature",
    fix: "kind/bugfix",
    chore: "kind/chore",
    docs: "area/documentation",
    test: "area/testing",
    perf: "area/performance",
  },
  scopeToLabel: {
    deps: "kind/dependency",
  },
  breakingLabel: "!BREAKING-CHANGE!",
};

test("type-only title maps to the type label", () => {
  const { valid, labels } = deriveLabels("feat: add feature", maps);
  assert.strictEqual(valid, true);
  assert.deepStrictEqual(labels, ["kind/feature"]);
});

test("known scope adds the scope label in addition to the type label", () => {
  const { valid, labels } = deriveLabels("chore(deps): bump x", maps);
  assert.strictEqual(valid, true);
  assert.deepStrictEqual(labels, ["kind/chore", "kind/dependency"]);
});

test("unknown scope is ignored, only the type label is applied", () => {
  const { valid, labels } = deriveLabels("feat(cli): add flag", maps);
  assert.strictEqual(valid, true);
  assert.deepStrictEqual(labels, ["kind/feature"]);
});

// Regression: a scope that collides with an Object.prototype property name
// (e.g. "constructor") must not resolve to the inherited property and push a
// non-string label. This previously caused addLabels to fail with HTTP 422.
test("scope 'constructor' does not leak an inherited prototype property", () => {
  const { valid, labels } = deriveLabels("feat(constructor): add field", maps);
  assert.strictEqual(valid, true);
  assert.deepStrictEqual(labels, ["kind/feature"]);
});

test("scope 'toString' does not leak an inherited prototype property", () => {
  const { valid, labels } = deriveLabels("fix(toString): correct output", maps);
  assert.strictEqual(valid, true);
  assert.deepStrictEqual(labels, ["kind/bugfix"]);
});

test("breaking change adds the breaking label first", () => {
  const { valid, labels } = deriveLabels("feat(deps)!: drop old API", maps);
  assert.strictEqual(valid, true);
  assert.deepStrictEqual(labels, ["!BREAKING-CHANGE!", "kind/feature", "kind/dependency"]);
});

test("special 'Initial commit' title is valid but yields no labels", () => {
  const { valid, labels } = deriveLabels("Initial commit", maps);
  assert.strictEqual(valid, true);
  assert.deepStrictEqual(labels, []);
});

test("'Merge' title is valid but yields no labels", () => {
  const { valid, labels } = deriveLabels("Merge branch 'main' into feature", maps);
  assert.strictEqual(valid, true);
  assert.deepStrictEqual(labels, []);
});

test("invalid title format is reported as not valid", () => {
  const { valid, labels } = deriveLabels("no conventional prefix here", maps);
  assert.strictEqual(valid, false);
  assert.deepStrictEqual(labels, []);
});

test("unknown type is treated as an invalid title (not in allowed types)", () => {
  const { valid, labels } = deriveLabels("wip: something", maps);
  assert.strictEqual(valid, false);
  assert.deepStrictEqual(labels, []);
});

// A type that collides with an Object.prototype name must be rejected by the
// regex (type is constrained to the allowed set), so it never reaches the map
// lookup. This documents where the type-position defense lives.
test("type colliding with a prototype name is rejected by the regex", () => {
  const { valid, labels } = deriveLabels("constructor: add field", maps);
  assert.strictEqual(valid, false);
  assert.deepStrictEqual(labels, []);
});

test("nullProtoMap does not resolve inherited Object.prototype members", () => {
  const m = nullProtoMap({ deps: "kind/dependency" });
  assert.strictEqual(m.deps, "kind/dependency");
  assert.strictEqual(m.constructor, undefined);
  assert.strictEqual(m.toString, undefined);
  assert.strictEqual(m.hasOwnProperty, undefined);
});
