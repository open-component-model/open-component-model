// @ts-check
import assert from "assert";
import { mkdtempSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import {
  extractPackageSummary,
  GENERATED_TABLE_END,
  GENERATED_TABLE_START,
  generatePackagesReadme,
} from "./dependency-list.js";

const GOLANGCI_YML = `
linters:
  settings:
    depguard:
      rules:
        beta:
          list-mode: lax
          files: ["**/bindings/go/beta/**"]
          allow:
            - "ocm.software/open-component-model/bindings/go/beta"
            - "ocm.software/open-component-model/bindings/go/alpha"
        alpha:
          list-mode: lax
          files: ["**/bindings/go/alpha/**"]
          allow:
            - "ocm.software/open-component-model/bindings/go/alpha"
`;

const README = `# Bindings

## Packages

${GENERATED_TABLE_START}

stale content

${GENERATED_TABLE_END}

## Other section
`;

function setup() {
  const dir = mkdtempSync(join(tmpdir(), "dependency-list-"));
  writeFileSync(join(dir, "golangci.yml"), GOLANGCI_YML);
  writeFileSync(join(dir, "README.md"), README);
  return {
    config: join(dir, "golangci.yml"),
    readme: join(dir, "README.md"),
  };
}

function addBinding(dir, name, docComment) {
  const pkgDir = join(dir, name);
  mkdirSync(pkgDir, { recursive: true });
  writeFileSync(join(pkgDir, "doc.go"), `${docComment}\npackage ${name}\n`);
}

test("extractPackageSummary takes the first sentence and strips the package prefix", () => {
  const dir = mkdtempSync(join(tmpdir(), "dependency-list-"));
  const docGo = join(dir, "doc.go");
  writeFileSync(
    docGo,
    "// Package blob provides blob handling for artifact content. This sentence is ignored.\n//\n// More details follow.\npackage blob\n"
  );
  assert.strictEqual(
    extractPackageSummary(docGo),
    "Provides blob handling for artifact content"
  );
});

test("extractPackageSummary strips a command prefix and joins wrapped lines", () => {
  const dir = mkdtempSync(join(tmpdir(), "dependency-list-"));
  const docGo = join(dir, "doc.go");
  writeFileSync(
    docGo,
    "// Command ocm provides the command line interface\n// for working with the Open Component Model.\npackage main\n"
  );
  assert.strictEqual(
    extractPackageSummary(docGo),
    "Provides the command line interface for working with the Open Component Model"
  );
});

test("generatePackagesReadme replaces only the marked block and sorts packages", () => {
  const { config, readme } = setup();
  const bindingsDir = join(readme, "..");
  addBinding(bindingsDir, "alpha", "// Package alpha provides alpha support.");
  addBinding(bindingsDir, "beta", "// Package beta provides beta support.");

  const result = generatePackagesReadme(config, readme);

  assert.ok(result.includes("| **alpha** | Provides alpha support |"));
  assert.ok(result.includes("| **beta** | Provides beta support |"));
  assert.ok(result.indexOf("**alpha**") < result.indexOf("**beta**"));
  assert.ok(!result.includes("stale content"));
  assert.ok(result.includes("## Packages"));
  assert.ok(result.includes("## Other section"));
  // The surrounding document is left untouched.
  assert.ok(result.startsWith("# Bindings\n"));
  assert.strictEqual(readFileSync(readme, "utf-8"), README);
});

test("generatePackagesReadme fails when a binding has no doc.go", () => {
  const { config, readme } = setup();
  addBinding(join(readme, ".."), "alpha", "// Package alpha provides alpha support.");

  assert.throws(
    () => generatePackagesReadme(config, readme),
    /missing doc\.go for depguard bindings: beta/
  );
});

test("generatePackagesReadme fails when the markers are missing", () => {
  const { config, readme } = setup();
  const bindingsDir = join(readme, "..");
  addBinding(bindingsDir, "alpha", "// Package alpha provides alpha support.");
  addBinding(bindingsDir, "beta", "// Package beta provides beta support.");
  writeFileSync(readme, "# Bindings\n");

  assert.throws(
    () => generatePackagesReadme(config, readme),
    /does not contain the generated packages table markers/
  );
});
