// @ts-check

import assert from "node:assert";
import { test } from "node:test";
import {
  slugify,
  parseFrontMatter,
  pageUrl,
  walkMd,
  collectAliases,
} from "./check-website-redirects.js";
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";

// ---------------------------------------------------------------------------
// slugify
// ---------------------------------------------------------------------------

test("slugify lowercases and replaces spaces with hyphens", () => {
  assert.strictEqual(slugify("Transfer Components across an Air Gap"), "transfer-components-across-an-air-gap");
});

test("slugify collapses multiple non-alphanumeric chars into one hyphen", () => {
  assert.strictEqual(slugify("Certificate Chains (PEM)"), "certificate-chains-pem");
});

test("slugify strips trailing hyphen left by closing paren", () => {
  assert.strictEqual(slugify("Sigstore (Keyless)"), "sigstore-keyless");
});

test("slugify keeps acronyms lowercase and intact", () => {
  assert.strictEqual(slugify("How to use the OCM CLI container image"), "how-to-use-the-ocm-cli-container-image");
});

test("slugify handles double-dash in title (e.g. --flag)", () => {
  assert.strictEqual(slugify("Migrate from --upload-as to Uploader Configurations"), "migrate-from-upload-as-to-uploader-configurations");
});

test("slugify strips leading hyphen", () => {
  assert.strictEqual(slugify("(Leading paren)"), "leading-paren");
});

test("slugify of directory segment with spaces and mixed case", () => {
  assert.strictEqual(slugify("Sign and Verify"), "sign-and-verify");
});

// ---------------------------------------------------------------------------
// parseFrontMatter
// ---------------------------------------------------------------------------

test("parseFrontMatter extracts title and slug", () => {
  const content = `---\ntitle: "My Page"\nslug: my-slug\n---\nBody`;
  const fm = parseFrontMatter(content);
  assert.strictEqual(fm.title, "My Page");
  assert.strictEqual(fm.slug, "my-slug");
});

test("parseFrontMatter returns empty object when no front matter", () => {
  const fm = parseFrontMatter("# Just markdown\nNo front matter here.");
  assert.deepStrictEqual(fm, {});
});

test("parseFrontMatter returns empty object for empty front matter block", () => {
  const fm = parseFrontMatter("---\n---\nBody");
  assert.deepStrictEqual(fm, {});
});

test("parseFrontMatter handles aliases list", () => {
  const content = `---\ntitle: "Page"\naliases:\n  - /old/path/\n  - /other/path/\n---\n`;
  const fm = parseFrontMatter(content);
  assert.deepStrictEqual(fm.aliases, ["/old/path/", "/other/path/"]);
});

// ---------------------------------------------------------------------------
// pageUrl
// ---------------------------------------------------------------------------

test("pageUrl derives URL from title when no slug", () => {
  const url = pageUrl("how-to/air-gap-transfer.md", { title: "Transfer Components across an Air Gap" });
  assert.strictEqual(url, "/docs/how-to/transfer-components-across-an-air-gap/");
});

test("pageUrl prefers slug over title", () => {
  const url = pageUrl("how-to/sign-component-version.md", {
    title: "Sign Component Versions (old title)",
    slug: "sign-component-versions",
  });
  assert.strictEqual(url, "/docs/how-to/sign-component-versions/");
});

test("pageUrl slugifies directory segments", () => {
  const url = pageUrl(
    "how-to/Sign and Verify/configure-signing-credentials.md",
    { title: "Configure Credentials for Signing" },
  );
  assert.strictEqual(url, "/docs/how-to/sign-and-verify/configure-credentials-for-signing/");
});

test("pageUrl handles nested directories", () => {
  const url = pageUrl(
    "how-to/vendor-specific-apis/jfrog-artifactory/helm-charts.md",
    { title: "Upload Helm Charts to JFrog Artifactory", slug: "helm-charts" },
  );
  assert.strictEqual(url, "/docs/how-to/vendor-specific-apis/jfrog-artifactory/helm-charts/");
});

test("pageUrl handles signing subdirectory with parentheses in title", () => {
  const url = pageUrl("tutorials/signing/pem.md", { title: "Certificate Chains (PEM)" });
  assert.strictEqual(url, "/docs/tutorials/signing/certificate-chains-pem/");
});

test("pageUrl falls back to filename when title is missing", () => {
  const url = pageUrl("how-to/some-page.md", {});
  assert.strictEqual(url, "/docs/how-to/some-page/");
});

// ---------------------------------------------------------------------------
// collectAliases
// ---------------------------------------------------------------------------

function makeTmpDocs(files) {
  const dir = mkdtempSync(join(tmpdir(), "ocm-redirects-test-"));
  for (const [rel, content] of Object.entries(files)) {
    const full = join(dir, rel);
    mkdirSync(full.replace(/\/[^/]+$/, ""), { recursive: true });
    writeFileSync(full, content);
  }
  return dir;
}

test("collectAliases gathers aliases from multiple files", () => {
  const dir = makeTmpDocs({
    "guides/pack/page-a.md": `---\ntitle: "A"\naliases:\n  - /old/a/\n---\n`,
    "guides/transfer/page-b.md": `---\ntitle: "B"\naliases:\n  - /old/b/\n  - /also-old/b/\n---\n`,
    "guides/sign/page-c.md": `---\ntitle: "C"\n---\nNo aliases here.\n`,
  });
  try {
    const aliases = collectAliases(dir);
    assert.ok(aliases.has("/old/a/"));
    assert.ok(aliases.has("/old/b/"));
    assert.ok(aliases.has("/also-old/b/"));
    assert.strictEqual(aliases.size, 3);
  } finally {
    rmSync(dir, { recursive: true });
  }
});

test("collectAliases ignores files without aliases block", () => {
  const dir = makeTmpDocs({
    "page.md": `---\ntitle: "No aliases"\n---\nContent.\n`,
  });
  try {
    const aliases = collectAliases(dir);
    assert.strictEqual(aliases.size, 0);
  } finally {
    rmSync(dir, { recursive: true });
  }
});

test("collectAliases walks nested directories", () => {
  const dir = makeTmpDocs({
    "a/b/c/deep.md": `---\ntitle: "Deep"\naliases:\n  - /deep/old/\n---\n`,
  });
  try {
    const aliases = collectAliases(dir);
    assert.ok(aliases.has("/deep/old/"));
  } finally {
    rmSync(dir, { recursive: true });
  }
});

// ---------------------------------------------------------------------------
// walkMd
// ---------------------------------------------------------------------------

test("walkMd finds all .md files recursively", () => {
  const dir = makeTmpDocs({
    "a.md": "# A",
    "sub/b.md": "# B",
    "sub/deep/c.md": "# C",
    "not-markdown.txt": "ignored",
  });
  try {
    const files = walkMd(dir);
    const names = files.map((f) => f.replace(dir + "/", "")).sort();
    assert.deepStrictEqual(names, ["a.md", "sub/b.md", "sub/deep/c.md"]);
  } finally {
    rmSync(dir, { recursive: true });
  }
});
