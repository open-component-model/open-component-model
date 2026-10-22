// @ts-check

/**
 * Static redirect coverage check for the OCM website.
 *
 * For every how-to and tutorials page that existed in the previously-released
 * website version (identified by the git tag `website/v<defaultContentVersion>`
 * in hugo.yaml), the script computes the live URL that Hugo would have served
 * (title-derived slug, or explicit `slug:` front-matter override, with all
 * directory segments slugified the same way). It then verifies that the current
 * branch has an `aliases:` entry in some docs page that covers every such URL.
 *
 * Run:
 *   node .github/scripts/check-website-redirects.js [repo-root]
 *
 * Exits 1 if any previously-live URL is not covered; 0 otherwise.
 */

import { execSync, spawnSync } from "node:child_process";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join, resolve } from "node:path";
import { pathToFileURL } from "node:url";
import { load as yamlLoad } from "js-yaml";

// ---------------------------------------------------------------------------
// URL derivation
// ---------------------------------------------------------------------------

/**
 * Hugo's default slugify: lowercase, replace runs of non-alphanumeric chars
 * with a single hyphen, strip leading/trailing hyphens.
 *
 * Verified against live ocm.software URLs for all known edge cases:
 * - "Certificate Chains (PEM)"  → "certificate-chains-pem"
 * - "Sigstore (Keyless)"        → "sigstore-keyless"
 * - "Sign and Verify" (dir)     → "sign-and-verify"
 * - "How to use the OCM CLI…"   → "how-to-use-the-ocm-cli-…"
 *
 * @param {string} s
 * @returns {string}
 */
export function slugify(s) {
  return s
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

/**
 * Parse the YAML front matter from a Markdown file's content string.
 *
 * @param {string} content
 * @returns {Record<string, unknown>}
 */
export function parseFrontMatter(content) {
  const match = content.match(/^---\n([\s\S]*?)\n---/);
  if (!match) return {};
  return /** @type {Record<string, unknown>} */ (yamlLoad(match[1]) ?? {});
}

/**
 * Derive the canonical live URL for a docs page given its path (relative to
 * `website/content/docs/`) and its front matter.
 *
 * Hugo resolution order: `slug:` front-matter wins; otherwise the title is
 * slugified. Every directory segment on the path is also slugified.
 *
 * @param {string} relPath  e.g. "how-to/Sign and Verify/configure-signing-credentials.md"
 * @param {Record<string, unknown>} fm  parsed front matter
 * @returns {string}  e.g. "/docs/how-to/sign-and-verify/configure-credentials-for-signing/"
 */
export function pageUrl(relPath, fm) {
  const parts = relPath.replace(/\.md$/, "").split("/");
  const dirParts = parts.slice(0, -1).map(slugify);
  const leaf = parts[parts.length - 1];
  const pageSlug =
    typeof fm.slug === "string" && fm.slug
      ? fm.slug
      : slugify(typeof fm.title === "string" && fm.title ? fm.title : leaf);
  return "/docs/" + [...dirParts, pageSlug].join("/") + "/";
}

// ---------------------------------------------------------------------------
// File I/O helpers
// ---------------------------------------------------------------------------

/**
 * Recursively collect all `.md` file paths under `dir`.
 *
 * @param {string} dir
 * @param {string[]} [acc]
 * @returns {string[]}
 */
export function walkMd(dir, acc = []) {
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) {
      walkMd(full, acc);
    } else if (entry.endsWith(".md")) {
      acc.push(full);
    }
  }
  return acc;
}

/**
 * Collect every `aliases:` entry from all Markdown files under `docsDir`.
 *
 * @param {string} docsDir  absolute path to `website/content/docs`
 * @returns {Set<string>}
 */
export function collectAliases(docsDir) {
  const aliases = new Set();
  for (const file of walkMd(docsDir)) {
    const content = readFileSync(file, "utf8");
    const fm = parseFrontMatter(content);
    if (Array.isArray(fm.aliases)) {
      for (const a of fm.aliases) {
        if (typeof a === "string") aliases.add(a);
      }
    }
  }
  return aliases;
}

// ---------------------------------------------------------------------------
// Base-ref URL discovery
// ---------------------------------------------------------------------------

/**
 * Read the `defaultContentVersion` from `website/config/_default/hugo.yaml`
 * and return the corresponding git tag name (`website/v<version>.1`).
 *
 * @param {string} repoRoot
 * @returns {string}  e.g. "website/v0.19.1"
 */
export function baseTag(repoRoot) {
  const hugoYaml = readFileSync(
    join(repoRoot, "website/config/_default/hugo.yaml"),
    "utf8",
  );
  const cfg = /** @type {Record<string, unknown>} */ (yamlLoad(hugoYaml) ?? {});
  const version = cfg["defaultContentVersion"];
  if (typeof version !== "string" || !version) {
    throw new Error(
      "Could not read defaultContentVersion from website/config/_default/hugo.yaml",
    );
  }
  return `website/v${version}.1`;
}

/**
 * List all non-`_index.md` Markdown files under the entire `docs/` tree
 * at the given git ref.
 *
 * @param {string} repoRoot
 * @param {string} ref  git ref (tag or commit)
 * @returns {string[]}
 */
export function listBaseFiles(repoRoot, ref) {
  const out = execSync(
    `git ls-tree -r --name-only "${ref}" -- website/content/docs/`,
    { cwd: repoRoot },
  ).toString();
  return out
    .trim()
    .split("\n")
    .filter((f) => f.endsWith(".md") && !f.endsWith("_index.md"));
}

/**
 * Read a file at a git ref. Uses `spawnSync` to avoid shell quoting issues
 * with paths that contain spaces.
 *
 * @param {string} repoRoot
 * @param {string} ref
 * @param {string} filePath  repo-relative path
 * @returns {string}
 */
export function gitShow(repoRoot, ref, filePath) {
  const result = spawnSync("git", ["show", `${ref}:${filePath}`], {
    cwd: repoRoot,
    encoding: "utf8",
  });
  if (result.status !== 0) {
    throw new Error(
      `git show ${ref}:${filePath} failed: ${result.stderr.trim()}`,
    );
  }
  return result.stdout;
}

// ---------------------------------------------------------------------------
// Main check
// ---------------------------------------------------------------------------

/**
 * Compute the expected live URLs from the base git tag and return those not
 * covered by an alias in the current branch.
 *
 * @param {string} repoRoot
 * @returns {{ missing: {url: string, sourceFile: string}[], total: number }}
 */
export function checkRedirects(repoRoot) {
  const ref = baseTag(repoRoot);
  const files = listBaseFiles(repoRoot, ref);
  const docsDir = join(repoRoot, "website/content/docs");
  const aliases = collectAliases(docsDir);

  const missing = [];
  for (const file of files) {
    const relPath = file.replace("website/content/docs/", "");
    const content = gitShow(repoRoot, ref, file);
    const fm = parseFrontMatter(content);
    const url = pageUrl(relPath, fm);
    if (!aliases.has(url)) {
      missing.push({ url, sourceFile: file });
    }
  }

  return { missing, total: files.length };
}

// ---------------------------------------------------------------------------
// CLI entry point
// ---------------------------------------------------------------------------

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(process.argv[1]).href
) {
  const repoRoot = resolve(process.argv[2] ?? ".");
  const { missing, total } = checkRedirects(repoRoot);

  if (missing.length === 0) {
    console.log(
      `✓ All ${total} previously-live docs URLs are covered by redirect aliases.`,
    );
  } else {
    console.error(
      `✗ ${missing.length}/${total} previously-live URLs have no redirect alias on this branch:\n`,
    );
    for (const { url, sourceFile } of missing) {
      console.error(`  ${url}`);
      console.error(`    ← ${sourceFile}`);
    }
    console.error(
      "\nAdd each missing URL to the aliases: list of the destination page.",
    );
    process.exitCode = 1;
  }
}
