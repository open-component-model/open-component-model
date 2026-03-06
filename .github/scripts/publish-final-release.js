// @ts-check
import fs from "fs";
import path from "path";

// --------------------------
// GitHub Actions entrypoint
// --------------------------
/** @param {import('@actions/github-script').AsyncFunctionArguments} args */
export default async function publishFinalRelease({ github, context, core }) {
  const {
    FINAL_TAG: finalTag,
    FINAL_VERSION: finalVersion,
    RC_TAG: rcTag,
    IMAGE_REPO: imageRepo,
    CHART_REPO: chartRepo,
    IMAGE_DIGEST: imageDigest,
    CHART_DIR: chartDir,
    NOTES_FILE: notesFile,
    SET_LATEST: setLatest,
    HIGHEST_FINAL_VERSION: highestFinalVersion,
  } = process.env;

  if (!finalTag || !finalVersion || !rcTag || !chartDir || !notesFile) {
    core.setFailed("Missing required environment variables");
    return;
  }

  const isLatest = setLatest === "true";
  const notes = prepareReleaseNotes(notesFile, rcTag, finalTag);
  const release = await getOrCreateRelease(github, context, {
    finalTag,
    finalVersion,
    notes,
    isLatest,
  });
  await uploadChartAssets(github, context, core, release.id, chartDir);
  await writeSummary(core, {
    finalTag,
    rcTag,
    finalVersion,
    imageRepo,
    chartRepo,
    imageDigest,
    isLatest,
    highestFinalVersion,
    releaseUrl: release.html_url,
  });
}

// --------------------------
// Helpers
// --------------------------

/**
 * Read the RC changelog and rewrite the header for the final release.
 * Falls back to a simple "Promoted from …" message if the file is missing.
 *
 * @param {string} notesFile - Path to the changelog markdown file.
 * @param {string} rcTag - The RC tag being promoted (e.g. "kubernetes/controller/v0.1.0-rc.1").
 * @param {string} finalTag - The final tag (e.g. "kubernetes/controller/v0.1.0").
 * @returns {string} The release notes body.
 */
export function prepareReleaseNotes(notesFile, rcTag, finalTag) {
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
  return notes.replace(
    /^\[([^\]]+)\]\s*-\s*[\d-]+/,
    `[${finalTag}] - promoted from [${rcTag}] on ${today}`,
  );
}

/**
 * Get an existing release by tag or create a new one (idempotent for reruns).
 *
 * @param {object} github - Octokit instance.
 * @param {object} context - GitHub Actions context.
 * @param {object} opts
 * @param {string} opts.finalTag
 * @param {string} opts.finalVersion
 * @param {string} opts.notes
 * @param {boolean} opts.isLatest
 * @returns {Promise<{id: number, html_url: string}>}
 */
export async function getOrCreateRelease(github, context, opts) {
  const { finalTag, finalVersion, notes, isLatest } = opts;
  const repo = { owner: context.repo.owner, repo: context.repo.repo };
  const makeLatest = isLatest ? "true" : "false";

  try {
    const existing = await github.rest.repos.getReleaseByTag({
      ...repo,
      tag: finalTag,
    });
    const updated = await github.rest.repos.updateRelease({
      ...repo,
      release_id: existing.data.id,
      tag_name: finalTag,
      name: `Controller ${finalVersion}`,
      body: notes,
      prerelease: false,
      make_latest: makeLatest,
    });
    return { id: updated.data.id, html_url: updated.data.html_url };
  } catch (e) {
    if (e.status !== 404) throw e;
    const created = await github.rest.repos.createRelease({
      ...repo,
      tag_name: finalTag,
      name: `Controller ${finalVersion}`,
      body: notes,
      prerelease: false,
      make_latest: makeLatest,
    });
    return { id: created.data.id, html_url: created.data.html_url };
  }
}

/**
 * Upload .tgz chart files as release assets, replacing duplicates.
 *
 * @param {object} github - Octokit instance.
 * @param {object} context - GitHub Actions context.
 * @param {object} core - GitHub Actions core module.
 * @param {number} releaseId - The release to attach assets to.
 * @param {string} chartDir - Directory containing chart .tgz files.
 */
export async function uploadChartAssets(github, context, core, releaseId, chartDir) {
  const repo = { owner: context.repo.owner, repo: context.repo.repo };
  const existing = (
    await github.rest.repos.listReleaseAssets({
      ...repo,
      release_id: releaseId,
      per_page: 100,
    })
  ).data;

  const files = fs.readdirSync(chartDir).filter((f) => f.endsWith(".tgz"));
  for (const file of files) {
    const dup = existing.find((a) => a.name === file);
    if (dup) {
      core.info(`Replacing existing asset: ${file}`);
      await github.rest.repos.deleteReleaseAsset({
        ...repo,
        asset_id: dup.id,
      });
    }
    const data = fs.readFileSync(path.join(chartDir, file));
    await github.rest.repos.uploadReleaseAsset({
      ...repo,
      release_id: releaseId,
      name: file,
      data,
      headers: {
        "content-type": "application/gzip",
        "content-length": data.length,
      },
    });
  }
}

/**
 * Write a GitHub Actions job summary table.
 *
 * @param {object} core - GitHub Actions core module.
 * @param {object} data - Summary data fields.
 */
export async function writeSummary(core, data) {
  const {
    finalTag,
    rcTag,
    finalVersion,
    imageRepo,
    chartRepo,
    imageDigest,
    isLatest,
    highestFinalVersion,
    releaseUrl,
  } = data;

  const imageTags = isLatest
    ? `${imageRepo}:${finalVersion}, ${imageRepo}:latest`
    : `${imageRepo}:${finalVersion}`;

  await core.summary
    .addHeading("Final Release Published")
    .addTable([
      [
        { data: "Field", header: true },
        { data: "Value", header: true },
      ],
      ["Final Tag", finalTag],
      ["Promoted from RC", rcTag],
      ["Highest Final Version", highestFinalVersion || "(none)"],
      ["Image Tags", imageTags],
      ["Helm Chart", `${chartRepo}:${finalVersion}`],
      [
        "Image Digest",
        imageDigest ? imageDigest.substring(0, 19) + "..." : "N/A",
      ],
      ["GitHub Latest", isLatest ? "Yes" : "No (older version)"],
    ])
    .addEOL()
    .addLink("View Release", releaseUrl)
    .addEOL()
    .write();
}
