// @ts-check
import { execSync } from "child_process";

/**
 * Resolve latest RC tag metadata for a release branch + component.
 *
 * @param {string} branch
 * @param {string} componentPath
 * @returns {{ latestRcTag: string, latestRcVersion: string, latestPromotionVersion: string, latestPromotionTag: string }}
 */
export function resolveLatestRc(branch, componentPath) {
  const basePrefix = parseReleaseBranch(branch);

  if (!componentPath) {
    throw new Error("componentPath is required");
  }

  const tagPattern = `${componentPath}/v${basePrefix}.*-rc.*`;
  const latestRcTag = execSync(`git tag --list '${tagPattern}' | sort -V | tail -n1`).toString().trim();
  return deriveLatestRcMetadata(latestRcTag, componentPath);
}

// --------------------------
// GitHub Actions entrypoint
// --------------------------
// noinspection JSUnusedGlobalSymbols
/** @param {import('@actions/github-script').AsyncFunctionArguments} args */
export default async function resolveLatestRcAction({ core }) {
  const branch = process.env.BRANCH;
  const componentPath = process.env.COMPONENT_PATH;
  const releaseCandidate = `${process.env.RELEASE_CANDIDATE ?? "true"}`.toLowerCase() === "true";

  if (!branch || !componentPath) {
    core.setFailed("Missing BRANCH or COMPONENT_PATH");
    return;
  }

  try {
    const { latestRcTag, latestRcVersion, latestPromotionVersion, latestPromotionTag } = resolveLatestRc(branch, componentPath);

    const heading = latestRcTag
      ? "📦 Latest RC Resolution (for final promotion)"
      : "📦 Latest RC Resolution (no prior RC found — expected for initial RC runs)";

    core.setOutput("latest_rc_tag", latestRcTag);
    core.setOutput("latest_rc_version", latestRcVersion);
    core.setOutput("latest_promotion_version", latestPromotionVersion);
    core.setOutput("latest_promotion_tag", latestPromotionTag);

    if (!releaseCandidate) {
      await core.summary
        .addHeading(heading)
        .addTable([
          [{ data: "Field", header: true }, { data: "Value", header: true }],
          ["Release Branch", branch],
          ["Component Path", componentPath],
          ["Latest RC Tag", latestRcTag || "(none)"],
          ["Latest RC Version", latestRcVersion || "(none)"],
          ["Latest Promotion Version", latestPromotionVersion || "(none)"],
          ["Latest Promotion Tag", latestPromotionTag || "(none)"],
        ])
        .write();
    }
  } catch (error) {
    core.setFailed(error.message);
  }
}

/**
 * Parse release branches of form releases/v0.X
 * @param {string} branch
 * @returns {string} base prefix (e.g. 0.1)
 */
export function parseReleaseBranch(branch) {
  const match = /^releases\/v(0\.\d+)$/.exec(branch || "");
  if (!match) {
    throw new Error(`Invalid branch format: ${branch}`);
  }
  return match[1];
}

/**
 * Derive latest RC metadata from latest RC tag and component path.
 * @param {string} latestRcTag
 * @param {string} componentPath
 */
export function deriveLatestRcMetadata(latestRcTag, componentPath) {
  const latestRcVersion = latestRcTag ? latestRcTag.replace(`${componentPath}/v`, "") : "";
  const latestPromotionVersion = latestRcVersion ? latestRcVersion.replace(/-rc\.\d+$/, "") : "";
  const latestPromotionTag = latestRcTag
    ? `${componentPath}/v${latestPromotionVersion}`
    : "";

  return { latestRcTag, latestRcVersion, latestPromotionVersion, latestPromotionTag };
}
