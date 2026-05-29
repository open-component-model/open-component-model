// @ts-check
import assert from "assert";
import { parsePRTitle, validatePRTitle, labelPR } from "./label-pr.js";

// ----------------------------------------------------------
// Test fixtures
// ----------------------------------------------------------

const TYPE_TO_LABEL = {
  feat: "kind/feature",
  fix: "kind/bugfix",
  chore: "kind/chore",
  docs: "area/documentation",
  test: "area/testing",
  perf: "area/performance",
};
const SCOPE_TO_LABEL = { deps: "kind/dependency" };
const BREAKING_CHANGE_LABEL = "!BREAKING-CHANGE!";
const ALLOWED_TYPES = Object.keys(TYPE_TO_LABEL).join("|");

process.env.TYPE_TO_LABEL = JSON.stringify(TYPE_TO_LABEL);
process.env.SCOPE_TO_LABEL = JSON.stringify(SCOPE_TO_LABEL);
process.env.BREAKING_CHANGE_LABEL = BREAKING_CHANGE_LABEL;

/**
 * @param {string} title
 * @returns {{ github: any, context: any, addedLabels: string[] }}
 */
function makeMocks(title) {
  const addedLabels = /** @type {string[]} */ ([]);
  const github = {
    rest: {
      issues: {
        addLabels: async (/** @type {{ labels: string[] }} */ { labels }) => {
          addedLabels.push(...labels);
        },
      },
    },
  };
  const context = {
    payload: { pull_request: { title, number: 42 } },
    repo: { owner: "owner", repo: "repo" },
  };
  return { github, context, addedLabels };
}

/**
 * Runs labelPR and expects an Error to be thrown for an invalid title.
 * @param {string} title
 */
async function expectLabelPRError(title) {
  const { github, context } = makeMocks(title);
  await assert.rejects(
    () => labelPR({ github, context }),
    (err) => {
      assert.ok(err instanceof Error, "Should throw an Error");
      assert.ok(err.message.includes("Invalid PR title"), "Should throw with invalid title message");
      return true;
    }
  );
}

// ----------------------------------------------------------
// parsePRTitle — valid titles
// ----------------------------------------------------------
console.log("Testing parsePRTitle with valid titles...");

{
  const groups = parsePRTitle("feat: add login", ALLOWED_TYPES);
  assert.ok(groups, "feat: should match");
  assert.strictEqual(groups.type, "feat");
  assert.strictEqual(groups.scope, undefined);
  assert.strictEqual(groups.breaking, "");
  assert.strictEqual(groups.subject, "add login");
}

{
  const groups = parsePRTitle("fix(deps): bump lodash", ALLOWED_TYPES);
  assert.ok(groups, "fix(deps): should match");
  assert.strictEqual(groups.type, "fix");
  assert.strictEqual(groups.scope, "deps");
  assert.strictEqual(groups.breaking, "");
}

{
  const groups = parsePRTitle("feat!: drop Node 14", ALLOWED_TYPES);
  assert.ok(groups, "feat! should match");
  assert.strictEqual(groups.type, "feat");
  assert.strictEqual(groups.breaking, "!");
}

{
  const groups = parsePRTitle("chore(deps)!: remove deprecated API", ALLOWED_TYPES);
  assert.ok(groups, "chore(deps)! should match");
  assert.strictEqual(groups.type, "chore");
  assert.strictEqual(groups.scope, "deps");
  assert.strictEqual(groups.breaking, "!");
}

// ----------------------------------------------------------
// parsePRTitle — invalid titles
// ----------------------------------------------------------
console.log("Testing parsePRTitle with invalid titles...");

assert.strictEqual(parsePRTitle("this is not a conventional commit", ALLOWED_TYPES), null, "plain text should not match");
assert.strictEqual(parsePRTitle("WIP: something", ALLOWED_TYPES), null, "unknown type should not match");
assert.strictEqual(parsePRTitle("", ALLOWED_TYPES), null, "empty title should not match");
assert.strictEqual(parsePRTitle("feat : space before colon", ALLOWED_TYPES), null, "space before colon should not match");

// ----------------------------------------------------------
// validatePRTitle
// ----------------------------------------------------------
console.log("Testing validatePRTitle...");

/**
 * @param {string} title
 * @returns {string | null} The failure message, or null if setFailed was not called.
 */
function runValidate(title) {
  let failed = /** @type {string | null} */ (null);
  const core = { setFailed: (/** @type {string} */ msg) => { failed = msg; } };
  const context = { payload: { pull_request: { title } } };
  validatePRTitle({ core, context });
  return failed;
}

// All allowed types should pass
console.log("Testing validatePRTitle valid types...");
for (const type of Object.keys(TYPE_TO_LABEL)) {
  assert.strictEqual(runValidate(`${type}: some subject`), null, `${type}: should be valid`);
}

// Invalid title calls setFailed with the format message
console.log("Testing validatePRTitle invalid titles...");
assert.ok(runValidate("this is not valid")?.includes("Conventional Commit"), "invalid title should call setFailed");
assert.ok(runValidate("WIP: something")?.includes("Conventional Commit"), "unknown type should call setFailed");

// Type not in a restricted allowed set should fail
assert.strictEqual(parsePRTitle("chore: cleanup", "feat|fix"), null, "type not in allowed set should fail");

// ----------------------------------------------------------
// labelPR — type labels
// ----------------------------------------------------------
console.log("Testing labelPR type-based labels...");

for (const [type, label] of Object.entries(TYPE_TO_LABEL)) {
  const { github, context, addedLabels } = makeMocks(`${type}: some subject`);
  await labelPR({ github, context });
  assert.deepStrictEqual(addedLabels, [label], `${type}: should add ${label}`);
}

// ----------------------------------------------------------
// labelPR — scope labels
// ----------------------------------------------------------
console.log("Testing labelPR scope-based labels...");

{
  const { github, context, addedLabels } = makeMocks("chore(deps): bump lodash");
  await labelPR({ github, context });
  assert.deepStrictEqual(addedLabels, ["kind/chore", "kind/dependency"], "chore(deps) should add both labels");
}

{
  const { github, context, addedLabels } = makeMocks("feat(cli): add login command");
  await labelPR({ github, context });
  assert.deepStrictEqual(addedLabels, ["kind/feature"], "unknown scope should not add a scope label");
}

// ----------------------------------------------------------
// labelPR — breaking change label
// ----------------------------------------------------------
console.log("Testing labelPR breaking change label...");

{
  const { github, context, addedLabels } = makeMocks("feat!: drop support for Node 14");
  await labelPR({ github, context });
  assert.deepStrictEqual(addedLabels, [BREAKING_CHANGE_LABEL, "kind/feature"], "feat! should add breaking + feature labels");
}

{
  const { github, context, addedLabels } = makeMocks("fix(deps)!: remove deprecated API");
  await labelPR({ github, context });
  assert.deepStrictEqual(addedLabels, [BREAKING_CHANGE_LABEL, "kind/bugfix", "kind/dependency"], "fix(deps)! should add breaking + bugfix + dependency labels");
}

// ----------------------------------------------------------
// labelPR — special titles produce no labels
// ----------------------------------------------------------
console.log("Testing labelPR special titles...");

{
  const { github, context, addedLabels } = makeMocks("Initial commit");
  await labelPR({ github, context });
  assert.deepStrictEqual(addedLabels, [], "Initial commit should add no labels");
}

{
  const { github, context, addedLabels } = makeMocks("Merge branch 'main' into feat/my-feature");
  await labelPR({ github, context });
  assert.deepStrictEqual(addedLabels, [], "Merge commit should add no labels");
}

// ----------------------------------------------------------
// labelPR — invalid titles exit 1
// ----------------------------------------------------------
console.log("Testing labelPR invalid titles...");

await expectLabelPRError("this is not a conventional commit");
await expectLabelPRError("");

// ----------------------------------------------------------
// labelPR — type with no label mapping adds no labels
// ----------------------------------------------------------
console.log("Testing labelPR unlabeled but valid types...");

{
  const saved = process.env.TYPE_TO_LABEL;
  process.env.TYPE_TO_LABEL = JSON.stringify({ ...TYPE_TO_LABEL, refactor: "" });
  const { github, context, addedLabels } = makeMocks("refactor: clean up code");
  await labelPR({ github, context });
  assert.deepStrictEqual(addedLabels, [], "Type with empty label mapping should add no labels");
  process.env.TYPE_TO_LABEL = saved;
}

console.log("✅ All label-pr tests passed.");
