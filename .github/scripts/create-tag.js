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
 * @param {string[]} args - Git arguments.
 * @returns {string} Trimmed stdout.
 */
function defaultExecGit(args) {
  return execFileSync("git", args, { encoding: "utf-8", stdio: "pipe" }).trim();
}

// --------------------------
// RC tag entrypoint
// --------------------------

/**
 * Create an RC tag with a simple annotation message.
 * Idempotent: skips if the tag already exists.
 * Sets output `pushed=true` on success or idempotent skip.
 *
 * Expects env vars: TAG
 *
 * @param {object} args
 * @param {object} args.core - GitHub Actions core module.
 * @param {function} [args.execGit] - Git executor (for testing).
 */
export async function createRcTag({ core, execGit = defaultExecGit }) {
  const { TAG: tag } = process.env;

  if (!tag) {
    core.setFailed("Missing TAG environment variable");
    return;
  }

  if (tagExists(tag, execGit)) {
    const existingSha = resolveTagCommit(tag, execGit);
    const headSha = execGit(["rev-parse", "HEAD"]);
    if (existingSha !== headSha) {
      core.setFailed(`Tag ${tag} already exists but points to ${existingSha.substring(0, 7)}, expected HEAD ${headSha.substring(0, 7)}`);
      return;
    }
    core.info(`Tag ${tag} already exists at HEAD, skipping (idempotent)`);
    core.setOutput("pushed", "true");
    return;
  }

  const message = `Release candidate ${tag}`;
  createAndPushTag({ tag, commit: "HEAD", message, execGit });
  core.setOutput("pushed", "true");
  core.info(`✅ Created RC tag ${tag}`);
}

// --------------------------
// New release tag entrypoint
// --------------------------

/**
 * Create a new release tag pointing to the same commit as the RC tag.
 * Idempotent: succeeds if the new release tag already points to the correct commit.
 * Fails if the new release tag exists but points to a different commit.
 *
 * Expects env vars: RC_TAG, NEW_RELEASE_TAG
 *
 * @param {object} args
 * @param {object} args.core - GitHub Actions core module.
 * @param {function} [args.execGit] - Git executor (for testing).
 */
export async function createNewReleaseTag({ core, execGit = defaultExecGit }) {
  const { RC_TAG: rcTag, NEW_RELEASE_TAG: newReleaseTag } = process.env;

  if (!rcTag || !newReleaseTag) {
    core.setFailed("Missing RC_TAG or NEW_RELEASE_TAG environment variables");
    return;
  }

  let rcSha;
  try {
    rcSha = resolveTagCommit(rcTag, execGit);
  } catch (err) {
    core.setFailed(err.message);
    return;
  }

  if (tagExists(newReleaseTag, execGit)) {
    let existingSha;
    try {
      existingSha = resolveTagCommit(newReleaseTag, execGit);
    } catch (err) {
      core.setFailed(err.message);
      return;
    }

    if (existingSha === rcSha) {
      core.info(
        `Tag ${newReleaseTag} already exists at expected commit ${rcSha.substring(0, 7)}, continuing (idempotent rerun)`,
      );
      core.setOutput("pushed", "true");
      return;
    }

    core.setFailed(
      `Tag ${newReleaseTag} already exists but points to ${existingSha.substring(0, 7)}, expected ${rcSha.substring(0, 7)}`,
    );
    return;
  }

  createAndPushTag({
    tag: newReleaseTag,
    commit: rcSha,
    message: `Promote ${rcTag} to ${newReleaseTag}`,
    execGit,
  });
  core.setOutput("pushed", "true");
  core.info(`✅ Created new release tag ${newReleaseTag} from ${rcTag} (${rcSha.substring(0, 7)})`);
}
