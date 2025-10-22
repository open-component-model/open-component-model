// @ts-check
import {execSync} from "child_process";


// --------------------------
// GitHub Actions entrypoint
// --------------------------
// noinspection JSUnusedGlobalSymbols
/** @param {import('@actions/github-script').AsyncFunctionArguments} args */
export default async function computeRcVersion({ core }) {
    const componentPath = process.env.COMPONENT_PATH;
    const releaseBranch = process.env.BRANCH;
    if (!componentPath || !releaseBranch) {
        core.setFailed("Missing COMPONENT_PATH or BRANCH");
        return;
    }

    const basePrefix = parseBranch(releaseBranch);
    const tagPrefix = `${componentPath}/v`;

    const latestStable = run(core, `git tag --list '${tagPrefix}${basePrefix}.*' | sort -V | tail -n1`);
    const latestRc = run(core, `git tag --list '${tagPrefix}${basePrefix}.*-rc.*' | sort -V | tail -n1`);

    const { baseVersion, rcVersion } = computeNextVersions(basePrefix, latestStable, latestRc, (cmd) => run(core, cmd));

    const rcTag = `${tagPrefix}${rcVersion}`;
    const promotionTag = `${tagPrefix}${baseVersion}`;

    core.setOutput("new_tag", rcTag);
    core.setOutput("new_version", rcVersion);
    core.setOutput("base_version", baseVersion);
    core.setOutput("promotion_tag", promotionTag);

    // --------------------------
    // Step summary
    // --------------------------
    await core.summary
        .addHeading("📦 RC Version Computation")
        .addTable([
            [
                { data: "Field", header: true },
                { data: "Value", header: true },
            ],
            ["Component Path", componentPath],
            ["Release Branch", releaseBranch],
            ["Base Prefix", basePrefix],
            ["Latest Stable", latestStable || "(none)"],
            ["Latest RC", latestRc || "(none)"],
            ["Next Base Version", baseVersion],
            ["Next RC Version", rcVersion],
            ["RC Tag", rcTag],
            ["Promotion Tag", promotionTag],
        ])
        .write();
}

// --------------------------
// Core helpers
// --------------------------
export function run(core, cmd) {
  core.info(`> ${cmd}`);
  try {
    const out = execSync(cmd).toString().trim();
    if (out) core.info(`Output: ${out}`);
    return out;
  } catch (err) {
    core.warning(`Command failed: ${cmd}\n${err.message}`);
    return "";
  }
}

export function parseVersion(tag) {
  if (!tag) return [];
  const version = tag.replace(/^.*v/, "").replace(/-rc\.\d+$/, "");
  return version.split(".").map(Number);
}

export function parseBranch(branch) {
  const match = /^releases\/v(0\.\d+)/.exec(branch);
  if (!match) throw new Error(`Invalid branch format: ${branch}`);
  return match[1];
}

export function computeNextVersions(basePrefix, latestStable, latestRc, runFn = () => "") {
    let [major, minor, patch] =
        parseVersion(latestStable).length > 0
            ? parseVersion(latestStable)
            : basePrefix.split(".").map(Number).concat(0).slice(0, 3);

    let rc = 1;
    const sameBase =
        latestStable && latestRc && parseVersion(latestStable).join(".") === parseVersion(latestRc).join(".");

    if (!sameBase && isStableNewer(latestStable, latestRc)) {
        patch++;
    } else if (latestRc) {
        rc = (parseInt(latestRc.match(/-rc\.(\d+)/)?.[1] ?? "0", 10) || 0) + 1;
    }

    const baseVersion = `${major}.${minor}.${patch}`;
    const rcVersion = `${baseVersion}-rc.${rc}`;
    return { baseVersion, rcVersion };
}

/**
 * Determine whether the latest stable tag is newer than the latest RC tag.
 */
export function isStableNewer(stable, rc) {
    if (!stable) return false;
    if (!rc) return true;

    const stableParts = parseVersion(stable);
    const rcParts = parseVersion(rc);

    // Compare [major, minor, patch] lexicographically
    for (let i = 0; i < 3; i++) {
        const s = stableParts[i] || 0;
        const r = rcParts[i] || 0;
        if (s > r) return true;
        if (s < r) return false;
    }

    // Same base version → stable is not newer than RC
    return false;
}