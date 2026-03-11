// @ts-check

import assert from "node:assert/strict";
import { describe, it, mock } from "node:test";
import { verifyRegistry } from "./verify-registry.js";

describe("verifyRegistry", () => {
  it("returns no diff when registry matches actual modules", () => {
    const { missing, extra } = verifyRegistry(".github/go-modules.yml");
    assert.deepEqual(missing, [], "No modules should be missing from registry");
    assert.deepEqual(extra, [], "No extra modules should be in registry");
  });
});
