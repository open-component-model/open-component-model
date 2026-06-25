// @ts-check
import { execFileSync } from "child_process";

// --------------------------
// Helpers
// --------------------------

/**
 * Check whether a git tag exists locally.
 *
 * @param {string} tag - The tag name to check.
 * @param {function} [execGit] - Git executor (for testing).
 * @returns {boolean}
 */
export function tagExists(tag, execGit = defaultExecGit) {
  try {
    execGit(["rev-parse", `refs/tags/${tag}`]);
    return true;
  } catch {
    return false;
  }
}

/**
 * Resolve the commit SHA a tag points to (peeling through annotated tags).
 *
 * @param {string} tag - The tag to resolve.
 * @param {function} [execGit] - Git executor (for testing).
 * @returns {string} The commit SHA.
 * @throws {Error} If the tag cannot be resolved.
 */
export function resolveTagCommit(tag, execGit = defaultExecGit) {
  const sha = execGit(["rev-parse", `refs/tags/${tag}^{commit}`]);
  if (!sha) {
    throw new Error(`Could not resolve commit for tag ${tag}`);
  }
  return sha;
}

/**
 * Create a GPG-signed tag and push it to origin.
 *
 * @param {object} opts
 * @param {string} opts.tag - Tag name to create.
 * @param {string} opts.commit - Commit SHA to tag (use "HEAD" for current).
 * @param {string} opts.message - Tag annotation message.
 * @param {function} [opts.execGit] - Git executor (for testing).
 */
export function createAndPushTag({ tag, commit, message, execGit = defaultExecGit }) {
  if (commit === "HEAD") {
    execGit(["tag", "-s", tag, "-m", message]);
  } else {
    execGit(["tag", "-s", tag, commit, "-m", message]);
  }
  execGit(["push", "origin", `refs/tags/${tag}`]);
}

/**
 * Default git executor using child_process.execFileSync.
 *
 * Authenticates via GITHUB_TOKEN when present. This is required because the
 * checkout action sets persist-credentials: false (enforced by zizmor), so git
 * has no stored credentials for push. The token must be passed to the script
 * via the workflow step's env block.
 *
 * @param {string[]} args - Git arguments.
 * @returns {string} Trimmed stdout.
 */
function defaultExecGit(args) {
  const token = process.env.GITHUB_TOKEN;
  const authArgs = token
    ? ["-c", `http.extraHeader=Authorization: basic ${Buffer.from(`x-access-token:${token}`).toString("base64")}`]
    : [];
  return execFileSync("git", [...authArgs, ...args], { encoding: "utf-8", stdio: "pipe" }).trim();
}

// --------------------------
// Multi-tag helpers
// --------------------------

/**
 * Create one annotated tag pointing at HEAD. Idempotent.
 * Returns true if created or already at HEAD; false if a hard mismatch was reported.
 */
function tagAtHead({ core, tag, message, execGit }) {
  if (tagExists(tag, execGit)) {
    const existingSha = resolveTagCommit(tag, execGit);
    const headSha = execGit(["rev-parse", "HEAD"]);
    if (existingSha !== headSha) {
      core.setFailed(`Tag ${tag} already exists but points to ${existingSha.substring(0, 7)}, expected HEAD ${headSha.substring(0, 7)}`);
      return false;
    }
    core.info(`Tag ${tag} already exists at HEAD (idempotent)`);
    return true;
  }
  createAndPushTag({ tag, commit: "HEAD", message, execGit });
  core.info(`Created tag ${tag}`);
  return true;
}

/**
 * Create one annotated tag pointing at a specific commit. Idempotent.
 */
function tagAtCommit({ core, tag, commit, message, execGit }) {
  if (tagExists(tag, execGit)) {
    const existingSha = resolveTagCommit(tag, execGit);
    if (existingSha === commit) {
      core.info(`Tag ${tag} already at expected commit (idempotent)`);
      return true;
    }
    core.setFailed(`Tag ${tag} exists at ${existingSha.substring(0, 7)}, expected ${commit.substring(0, 7)}`);
    return false;
  }
  createAndPushTag({ tag, commit, message, execGit });
  core.info(`Created tag ${tag} at ${commit.substring(0, 7)}`);
  return true;
}

// --------------------------
// RC tags entrypoint (canonical + side tags)
// --------------------------

/**
 * Create RC tags for the unified release: the canonical v0.X.Y-rc.N tag and
 * any number of side tags (cli/v0.X.Y-rc.N, kubernetes/controller/v0.X.Y-rc.N,
 * etc.) all pointing at HEAD.
 *
 * Expects env vars:
 *   CANONICAL_TAG Required. The user-facing release tag (e.g. "v0.7.0-rc.1").
 *   ADDITIONAL_TAGS Optional. Comma-separated list of side tags to emit on the
 *                  same commit (e.g. "cli/v0.7.0-rc.1,kubernetes/controller/v0.7.0-rc.1").
 */
export async function createRcTags({ core, execGit = defaultExecGit }) {
  const { CANONICAL_TAG: canonicalTag, ADDITIONAL_TAGS: moduleTagsRaw } = process.env;

  if (!canonicalTag) {
    core.setFailed("Missing CANONICAL_TAG environment variable");
    return;
  }

  const moduleTags = (moduleTagsRaw || "").split(",").map(s => s.trim()).filter(Boolean);
  const targets = [
    { tag: canonicalTag, message: `Release candidate ${canonicalTag}` },
    ...moduleTags.map(tag => ({ tag, message: `Side tag for ${canonicalTag}` })),
  ];

  for (const { tag, message } of targets) {
    if (!tagAtHead({ core, tag, message, execGit })) return;
  }

  core.setOutput("pushed", "true");
}

// --------------------------
// Final release tags entrypoint (canonical + side tags)
// --------------------------

/**
 * Promote RC commit to final release tags: the canonical v0.X.Y plus any side
 * tags supplied. All point at the RC tag's commit.
 *
 * Expects env vars:
 *   RC_TAG               Required. Source RC tag to resolve a commit from.
 *   NEW_RELEASE_TAG      Required. The user-facing final tag (e.g. "v0.7.0").
 *   ADDITIONAL_TAGS          Optional. Comma-separated list of side tags to emit
 *                        at the same commit as NEW_RELEASE_TAG.
 */
export async function createNewReleaseTags({ core, execGit = defaultExecGit }) {
  const {
    RC_TAG: rcTag,
    NEW_RELEASE_TAG: newReleaseTag,
    ADDITIONAL_TAGS: moduleTagsRaw,
  } = process.env;

  if (!rcTag || !newReleaseTag) {
    core.setFailed("Missing RC_TAG or NEW_RELEASE_TAG");
    return;
  }

  let rcSha;
  try {
    rcSha = resolveTagCommit(rcTag, execGit);
  } catch (err) {
    core.setFailed(err.message);
    return;
  }

  const moduleTags = (moduleTagsRaw || "").split(",").map(s => s.trim()).filter(Boolean);
  const targets = [
    { tag: newReleaseTag, message: `Promote ${rcTag} to ${newReleaseTag}` },
    ...moduleTags.map(tag => ({ tag, message: `Side tag for ${newReleaseTag}` })),
  ];

  for (const { tag, message } of targets) {
    if (!tagAtCommit({ core, tag, commit: rcSha, message, execGit })) return;
  }

  core.setOutput("pushed", "true");
}
