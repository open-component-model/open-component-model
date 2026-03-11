// @ts-check

import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { isModuleAffected, resolveModules } from "./resolve-modules.js";

const registryPath = ".github/go-modules.yml";

describe("isModuleAffected", () => {
  it("matches files under module path", () => {
    assert.equal(isModuleAffected("bindings/go/blob", ["bindings/go/blob/blob.go"]), true);
  });

  it("does not match unrelated files", () => {
    assert.equal(isModuleAffected("bindings/go/blob", ["bindings/go/oci/client.go"]), false);
  });

  it("does not match partial path prefix", () => {
    assert.equal(isModuleAffected("bindings/go/oci", ["bindings/go/oci-extra/foo.go"]), false);
  });

  it("integration module triggers on parent changes", () => {
    assert.equal(isModuleAffected("bindings/go/oci/integration", ["bindings/go/oci/client.go"]), true);
  });

  it("integration module triggers on own changes", () => {
    assert.equal(
      isModuleAffected("bindings/go/oci/integration", ["bindings/go/oci/integration/test.go"]),
      true,
    );
  });
});

describe("resolveModules", () => {
  it("returns all modules when changedFiles is null", () => {
    const result = resolveModules(registryPath, null);
    assert.ok(result.allModules.length > 0);
    assert.deepEqual(result.filteredModules, result.allModules);
    assert.equal(result.envChanged, false);
  });

  it("filters to only changed modules", () => {
    const result = resolveModules(registryPath, ["bindings/go/blob/blob.go"]);
    assert.deepEqual(result.filteredModules, ["bindings/go/blob"]);
    assert.deepEqual(result.unitTestModules, ["bindings/go/blob"]);
    assert.deepEqual(result.integrationTestModules, []);
  });

  it("returns empty lists when no modules changed", () => {
    const result = resolveModules(registryPath, ["README.md"]);
    assert.deepEqual(result.filteredModules, []);
    assert.deepEqual(result.unitTestModules, []);
    assert.deepEqual(result.integrationTestModules, []);
  });

  it("lints all modules when .env changed", () => {
    const result = resolveModules(registryPath, [".env", "bindings/go/blob/blob.go"]);
    assert.equal(result.envChanged, true);
    assert.deepEqual(result.lintModules, result.allModules);
    assert.deepEqual(result.filteredModules, ["bindings/go/blob"]);
  });

  it("includes sparse extras for examples module", () => {
    const result = resolveModules(registryPath, null);
    assert.ok(result.sparseExtras["bindings/go/examples"]);
    assert.ok(result.sparseExtras["bindings/go/examples"].includes("bindings/go/blob"));
  });

  it("integration module triggered by parent change", () => {
    const result = resolveModules(registryPath, ["bindings/go/oci/client.go"]);
    assert.ok(result.filteredModules.includes("bindings/go/oci"));
    assert.ok(result.filteredModules.includes("bindings/go/oci/integration"));
    assert.ok(result.integrationTestModules.includes("bindings/go/oci/integration"));
  });
});
