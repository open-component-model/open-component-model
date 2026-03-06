// @ts-check
import { execFileSync } from "child_process";
import fs from "fs";

// --------------------------
// Shared helpers
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
 * Create an annotated tag and push it to origin.
 *
 * @param {object} opts
 * @param {string} opts.tag - Tag name to create.
 * @param {string} opts.commit - Commit SHA to tag (use "HEAD" for current).
 * @param {string} opts.message - Tag annotation message.
 * @param {function} [opts.execGit] - Git executor (for testing).
 */
export function createAndPushTag({ tag, commit, message, execGit = defaultExecGit }) {
  if (commit === "HEAD") {
    execGit(["tag", "-a", tag, "-m", message]);
  } else {
    execGit(["tag", "-a", tag, commit, "-m", message]);
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
 * Create an RC tag with the changelog as annotation.
 * Idempotent: skips if the tag already exists.
 * Sets output `pushed=true` on success or idempotent skip.
 *
 * Expects env vars: TAG, CHANGELOG_FILE
 *
 * @param {object} args
 * @param {object} args.core - GitHub Actions core module.
 * @param {function} [args.execGit] - Git executor (for testing).
 */
export async function createRcTag({ core, execGit = defaultExecGit }) {
  const { TAG: tag, CHANGELOG_FILE: changelogFile } = process.env;

  if (!tag || !changelogFile) {
    core.setFailed("Missing TAG or CHANGELOG_FILE environment variables");
    return;
  }

  if (tagExists(tag, execGit)) {
    core.info(`Tag ${tag} already exists, skipping (idempotent)`);
    core.setOutput("pushed", "true");
    return;
  }

  const message = readChangelog(changelogFile);
  createAndPushTag({ tag, commit: "HEAD", message, execGit });
  core.setOutput("pushed", "true");
  core.info(`✅ Created RC tag ${tag}`);
}

/**
 * Read changelog file content.
 *
 * @param {string} filePath
 * @returns {string}
 */
function readChangelog(filePath) {
  return fs.readFileSync(filePath, "utf8");
}

// --------------------------
// Final tag entrypoint
// --------------------------

/**
 * Create a final tag pointing to the same commit as the RC tag.
 * Idempotent: succeeds if the final tag already points to the correct commit.
 * Fails if the final tag exists but points to a different commit.
 *
 * Expects env vars: RC_TAG, FINAL_TAG
 *
 * @param {object} args
 * @param {object} args.core - GitHub Actions core module.
 * @param {function} [args.execGit] - Git executor (for testing).
 */
export async function createFinalTag({ core, execGit = defaultExecGit }) {
  const { RC_TAG: rcTag, FINAL_TAG: finalTag } = process.env;

  if (!rcTag || !finalTag) {
    core.setFailed("Missing RC_TAG or FINAL_TAG environment variables");
    return;
  }

  let rcSha;
  try {
    rcSha = resolveTagCommit(rcTag, execGit);
  } catch (err) {
    core.setFailed(err.message);
    return;
  }

  if (tagExists(finalTag, execGit)) {
    let existingSha;
    try {
      existingSha = resolveTagCommit(finalTag, execGit);
    } catch (err) {
      core.setFailed(err.message);
      return;
    }

    if (existingSha === rcSha) {
      core.info(
        `Tag ${finalTag} already exists at expected commit ${rcSha.substring(0, 7)}, continuing (idempotent rerun)`,
      );
      return;
    }

    core.setFailed(
      `Tag ${finalTag} already exists but points to ${existingSha.substring(0, 7)}, expected ${rcSha.substring(0, 7)}`,
    );
    return;
  }

  createAndPushTag({
    tag: finalTag,
    commit: rcSha,
    message: `Promote ${rcTag} to ${finalTag}`,
    execGit,
  });
  core.info(`✅ Created final tag ${finalTag} from ${rcTag} (${rcSha.substring(0, 7)})`);
}
