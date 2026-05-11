// @ts-check
import fs from "fs";
import path from "path";

// GitHub has a hard limit on the length of the changelog.
// We could use git-cliff's [limit_commits](https://git-cliff.org/docs/configuration/git#limit_commits) setting, however,
// that is a _hard_ limit. It will basically cut off at a preconfiugred number. Meaning if we have too many commits, the
// user will not know that it was cut off.
const GITHUB_RELEASE_BODY_LIMIT = 125000;
const MAX_RELEASE_BODY_LENGTH = GITHUB_RELEASE_BODY_LIMIT - 5000; // safety buffer
const TRUNCATION_NOTICE = `\n\n---\n\n*Release notes truncated to fit GitHub's ${GITHUB_RELEASE_BODY_LIMIT}-character body limit. See the source changelog or \`git log\` for the complete history.*`;

// --------------------------
// Helpers
// --------------------------

/**
 * Promote changelog from RC: Read RC changelog and rewrite header for the final release.
 * Falls back to a simple "Promoted from …" message if file is missing.
 *
 * The header pattern is derived dynamically from the RC tag, so it works for
 * any component prefix (cli/v…, kubernetes/controller/v…, etc.).
 *
 * @param {string} notesFile - Path to the changelog markdown file.
 * @param {string} rcTag - The RC tag being promoted (e.g. "kubernetes/controller/v0.1.0-rc.1").
 * @param {string} newReleaseTag - The new release tag (e.g. "kubernetes/controller/v0.1.0").
 * @returns {string} The release notes body.
 */
export function prepareReleaseNotes(notesFile, rcTag, newReleaseTag) {
  let notes;
  try {
    notes = fs.readFileSync(notesFile, "utf8").trim();
  } catch {
    notes = "";
  }

  if (!notes) {
    return `Promoted from ${rcTag}`;
  }

  const today = new Date().toISOString().split("T")[0];

  // Build a regex that matches the RC header line produced by git-cliff
  // and that works across different component naming patterns. For example:
  // Header: "## [kubernetes/controller/v0.1.0-rc.1] - 2026-03-08"
  // We escape the RC tag to ensure that characters like `.` and `/`
  // in the tag name are matched literally, not as regex metacharacters.
  const escapedRcTag = rcTag.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const rcHeaderPattern = new RegExp(`^## \\[${escapedRcTag}\\].*$`, "m");

  if (rcHeaderPattern.test(notes)) {
    notes = notes.replace(
        rcHeaderPattern,
        `## [${newReleaseTag}] - promoted from [${rcTag}] on ${today}`,
    );
  } else {
    // No RC header found — prepend a final header instead of failing.
    // Handles edge cases like manually edited release notes.
    notes = `## [${newReleaseTag}] - promoted from [${rcTag}] on ${today}\n\n${notes}`;
  }

  // GitHub rejects release bodies > 125000 chars. Truncate with a notice if
  // the content (typical for first-release-on-fresh-stream changelogs) tips over.
  if (notes.length > MAX_RELEASE_BODY_LENGTH) {
    let safeLength = MAX_RELEASE_BODY_LENGTH - TRUNCATION_NOTICE.length;
    // Avoid splitting a UTF-16 surrogate pair.
    const code = notes.charCodeAt(safeLength - 1);
    if (code >= 0xd800 && code <= 0xdbff) safeLength -= 1;
    notes = notes.substring(0, safeLength) + TRUNCATION_NOTICE;
  }

  return notes;
}

/**
 * Get an existing release by tag or create a new one (idempotent for reruns).
 *
 * @param {object} github - Octokit instance.
 * @param {object} context - GitHub Actions context.
 * @param {object} opts
 * @param {string} opts.newReleaseTag
 * @param {string} opts.newReleaseVersion
 * @param {string} opts.componentName
 * @param {string} opts.notes
 * @param {boolean} opts.isLatest
 * @returns {Promise<{id: number, html_url: string}>}
 */
export async function getOrCreateRelease(github, context, opts) {
  const { newReleaseTag, newReleaseVersion, componentName, notes, isLatest } = opts;
  const repo = { owner: context.repo.owner, repo: context.repo.repo };
  const makeLatest = isLatest ? "true" : "false";
  const releaseName = `${componentName} ${newReleaseVersion}`;

  try {
    const existing = await github.rest.repos.getReleaseByTag({
      ...repo,
      tag: newReleaseTag,
    });
    const updated = await github.rest.repos.updateRelease({
      ...repo,
      release_id: existing.data.id,
      tag_name: newReleaseTag,
      name: releaseName,
      body: notes,
      prerelease: false,
      make_latest: makeLatest,
    });
    return { id: updated.data.id, html_url: updated.data.html_url };
  } catch (e) {
    if (e.status !== 404) throw e;
    const created = await github.rest.repos.createRelease({
      ...repo,
      tag_name: newReleaseTag,
      name: releaseName,
      body: notes,
      prerelease: false,
      make_latest: makeLatest,
    });
    return { id: created.data.id, html_url: created.data.html_url };
  }
}

/**
 * Upload all files from assets directory as release assets, replacing duplicates.
 *
 * @param {object} github - Octokit instance.
 * @param {object} context - GitHub Actions context.
 * @param {object} core - GitHub Actions core module.
 * @param {number} releaseId - The release to attach assets to.
 * @param {string} assetsDir - Directory containing files to upload.
 * @returns {Promise<number>} Number of uploaded files.
 */
export async function uploadAssets(github, context, core, releaseId, assetsDir) {
  const repo = { owner: context.repo.owner, repo: context.repo.repo };
  const existing = (
    await github.rest.repos.listReleaseAssets({
      ...repo,
      release_id: releaseId,
      per_page: 100, // Note: does not paginate — assumes ≤100 assets per release
    })
  ).data;

  const files = fs.readdirSync(assetsDir).filter((f) => {
    const stat = fs.statSync(path.join(assetsDir, f));
    return stat.isFile();
  });

  for (const file of files) {
    const dup = existing.find((a) => a.name === file);
    if (dup) {
      core.info(`Replacing existing asset: ${file}`);
      await github.rest.repos.deleteReleaseAsset({
        ...repo,
        asset_id: dup.id,
      });
    }
    const data = fs.readFileSync(path.join(assetsDir, file));
    await github.rest.repos.uploadReleaseAsset({
      ...repo,
      release_id: releaseId,
      name: file,
      data,
      headers: {
        "content-type": "application/octet-stream",
        "content-length": data.length,
      },
    });
    core.info(`Uploaded: ${file}`);
  }

  return files.length;
}

/**
 * Write a GitHub Actions job summary table.
 *
 * @param {object} core - GitHub Actions core module.
 * @param {object} data - Summary data fields.
 */
export async function writeSummary(core, data) {
  const {
    newReleaseTag,
    rcTag,
    newReleaseVersion,
    componentName,
    imageRepo,
    chartRepo,
    imageDigest,
    isLatest,
    highestPreviousReleaseVersion,
    uploadedCount,
    releaseUrl,
  } = data;

  const rows = [
    [
      { data: "Field", header: true },
      { data: "Value", header: true },
    ],
    ["Component", componentName],
    ["New Release Tag", newReleaseTag],
    ["Promoted from RC", rcTag],
    ["Uploaded Assets", String(uploadedCount)],
  ];

  if (highestPreviousReleaseVersion) {
    rows.push(["Highest Previous Release Version", highestPreviousReleaseVersion]);
  }

  // Add optional OCI/Helm fields when present
  if (imageRepo) {
    const imageTags = isLatest
      ? `${imageRepo}:${newReleaseVersion}, ${imageRepo}:latest`
      : `${imageRepo}:${newReleaseVersion}`;
    rows.push(["Image Tags", imageTags]);
  }
  if (imageDigest) {
    rows.push(["Image Digest", imageDigest.substring(0, 19) + "..."]);
  }
  if (chartRepo) {
    rows.push(["Helm Chart", `${chartRepo}:${newReleaseVersion}`]);
  }
  rows.push(["GitHub Latest", isLatest ? "✅ yes" : "⚠️ no"]);

  await core.summary
    .addHeading("Final Release Published")
    .addTable(rows)
    .addEOL()
    .addLink("View Release", releaseUrl)
    .addEOL()
    .write();
}

// --------------------------
// GitHub Actions entrypoint
// --------------------------

/**
 * Publish a new GitHub release by promoting an RC.
 *
 * Required env vars:
 *   NEW_RELEASE_TAG, NEW_RELEASE_VERSION, RC_TAG, COMPONENT_NAME, ASSETS_DIR, NOTES_FILE
 *
 * Optional env vars (for summary):
 *   IMAGE_REPO, IMAGE_DIGEST, CHART_REPO
 *
 * @param {import('@actions/github-script').AsyncFunctionArguments} args
 */
export default async function publishFinalRelease({ github, context, core }) {
  const {
    NEW_RELEASE_TAG: newReleaseTag,
    NEW_RELEASE_VERSION: newReleaseVersion,
    RC_TAG: rcTag,
    COMPONENT_NAME: componentName,
    ASSETS_DIR: assetsDir,
    NOTES_FILE: notesFile,
    // Optional — only used in summary
    IMAGE_REPO: imageRepo,
    IMAGE_DIGEST: imageDigest,
    CHART_REPO: chartRepo,
    // Optional — controls GitHub "Latest" badge and :latest OCI tag
    SET_LATEST: setLatest,
    HIGHEST_PREVIOUS_RELEASE_VERSION: highestPreviousReleaseVersion,
  } = process.env;

  if (!newReleaseTag || !newReleaseVersion || !rcTag || !componentName || !assetsDir || !notesFile) {
    core.setFailed(
        "Missing required env vars: NEW_RELEASE_TAG, NEW_RELEASE_VERSION, RC_TAG, COMPONENT_NAME, ASSETS_DIR, NOTES_FILE",
    );
    return;
  }

  const isLatest = setLatest === "true";
  const notes = prepareReleaseNotes(notesFile, rcTag, newReleaseTag);
  const release = await getOrCreateRelease(github, context, {
    newReleaseTag,
    newReleaseVersion,
    componentName,
    notes,
    isLatest,
  });
  const uploadedCount = await uploadAssets(github, context, core, release.id, assetsDir);
  await writeSummary(core, {
    newReleaseTag,
    rcTag,
    newReleaseVersion,
    componentName,
    imageRepo,
    chartRepo,
    imageDigest,
    isLatest,
    highestPreviousReleaseVersion: highestPreviousReleaseVersion || "",
    uploadedCount,
    releaseUrl: release.html_url,
  });
}
