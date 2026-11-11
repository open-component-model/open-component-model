// @ts-check
// Shared single-file publication helpers for the gh-pages release feed.
//
// Exported functions are used via `actions/github-script`; they accept the
// action-provided { github, context, core } object and operate only on the
// file `releases.xml` on the `gh-pages` branch. They never replace a branch
// tree, create a new Pages branch, or touch source branches.

import { readFileSync } from "node:fs";

const FEED_PATH = "releases.xml";
const PAGES_BRANCH = "gh-pages";
const COMMIT_MESSAGE = "chore: update release notification feed";

// ---------------------------------------------------------------------------
// Internal shared helper: read the current published feed bytes from gh-pages.
//
// Returns: { buf: Buffer, sha: string } if the file exists and is readable.
//          null if the file is genuinely absent (branch exists, file does not).
// Throws on: missing branch, non-file response, encoding:none fetch failure,
//            permission/network errors, or unreadable content.
// ---------------------------------------------------------------------------

/**
 * @param {{ github: any, context: any, core: any }} ctx
 * @returns {Promise<{ buf: Buffer, sha: string } | null>}
 */
async function readPublishedFeed({ github, context }) {
    let data;
    try {
        const response = await github.rest.repos.getContent({
            ...context.repo,
            path: FEED_PATH,
            ref: PAGES_BRANCH,
            headers: { accept: "application/vnd.github.object+json" },
        });
        data = response.data;
    } catch (err) {
        if (err.status === 404) {
            // Distinguish: is the branch itself absent, or just the file?
            try {
                await github.rest.repos.getBranch({
                    ...context.repo,
                    branch: PAGES_BRANCH,
                });
                // Branch exists → file is genuinely absent
                return null;
            } catch (branchErr) {
                throw new Error(
                    `Pages branch '${PAGES_BRANCH}' does not exist or is inaccessible: ${branchErr.message}`,
                    { cause: branchErr }
                );
            }
        }
        throw new Error(`Failed to read ${FEED_PATH} from ${PAGES_BRANCH}: ${err.message}`, { cause: err });
    }

    if (data.type !== "file") {
        throw new Error(
            `Expected a file at ${FEED_PATH} on ${PAGES_BRANCH}, got type: ${data.type}`
        );
    }

    if (data.encoding === "base64") {
        return { buf: Buffer.from(data.content, "base64"), sha: data.sha };
    }

    if (data.encoding === "none") {
        // File too large for inline delivery; fetch immutable blob by SHA
        let blobResp;
        try {
            blobResp = await github.rest.git.getBlob({
                ...context.repo,
                file_sha: data.sha,
            });
        } catch (blobErr) {
            throw new Error(
                `Failed to fetch blob for ${FEED_PATH} (sha ${data.sha}): ${blobErr.message}`,
                { cause: blobErr }
            );
        }
        if (blobResp.data.encoding !== "base64") {
            throw new Error(
                `Unexpected blob encoding '${blobResp.data.encoding}' for ${FEED_PATH}`
            );
        }
        return { buf: Buffer.from(blobResp.data.content, "base64"), sha: data.sha };
    }

    throw new Error(
        `Unsupported encoding '${data.encoding}' for ${FEED_PATH} on ${PAGES_BRANCH}`
    );
}

// ---------------------------------------------------------------------------
// publishReleaseFeed
// ---------------------------------------------------------------------------

/**
 * Compare the generated feed at `inputPath` with the deployed feed on
 * `gh-pages`. If they differ (or no feed exists yet), commit the new file.
 *
 * @param {{ github: any, context: any, core: any }} ctx
 * @param {string} inputPath  Absolute path to the generated releases.xml.
 * @returns {Promise<boolean>} true if a commit was created, false if unchanged.
 */
export async function publishReleaseFeed({ github, context, core }, inputPath) {
    const newBytes = readFileSync(inputPath);

    const existing = await readPublishedFeed({ github, context });

    if (existing !== null && existing.buf.equals(newBytes)) {
        core.info("Release feed unchanged; no publication needed.");
        return false;
    }

    // author/committer are intentionally omitted: the Contents API defaults them
    // to the authenticated identity, which is the OCMBot GitHub App installation
    // token this workflow runs with. Do not hardcode a human-looking identity or a
    // DCO sign-off onto an automated gh-pages deploy commit.
    const params = {
        ...context.repo,
        path: FEED_PATH,
        branch: PAGES_BRANCH,
        message: COMMIT_MESSAGE,
        content: newBytes.toString("base64"),
    };

    if (existing !== null) {
        // Updating an existing file requires the current sha to detect conflicts
        params.sha = existing.sha;
    }

    const result = await github.rest.repos.createOrUpdateFileContents(params);
    core.info(`Release feed published: ${result.data.commit.html_url}`);
    return true;
}

// ---------------------------------------------------------------------------
// preserveReleaseFeed
// ---------------------------------------------------------------------------

/**
 * Copy the currently deployed `releases.xml` from `gh-pages` to a local path
 * so the website publisher includes it in its complete-site artifact.
 *
 * If the feed has not yet been published, leaves `outputPath` absent and
 * returns false. If the feed is absent but an unexpected local file already
 * exists at `outputPath`, throws rather than publishing an unowned file.
 *
 * @param {{ github: any, context: any, core: any }} ctx
 * @param {string} outputPath  Absolute path where the feed bytes should land.
 * @returns {Promise<boolean>} true if the file was written, false if absent.
 */
export async function preserveReleaseFeed({ github, context, core }, outputPath) {
    const { writeFileSync, existsSync } = await import("node:fs");

    const existing = await readPublishedFeed({ github, context });

    if (existing === null) {
        if (existsSync(outputPath)) {
            throw new Error(
                `Release feed is not yet published on ${PAGES_BRANCH}, but a file already exists at ${outputPath}. ` +
                `Refusing to publish an unowned file to the website.`
            );
        }
        core.info("Release feed not yet published; skipping preservation.");
        return false;
    }

    writeFileSync(outputPath, existing.buf);
    core.info(`Release feed preserved at ${outputPath} (${existing.buf.length} bytes)`);
    return true;
}
