import assert from "assert";
import { deriveLatestRcMetadata, parseReleaseBranch } from "./resolve-latest-rc.js";

// ----------------------------------------------------------
// parseReleaseBranch tests
// ----------------------------------------------------------
assert.strictEqual(parseReleaseBranch("releases/v0.1"), "0.1");
assert.throws(() => parseReleaseBranch("main"), /Invalid branch format/);
assert.throws(() => parseReleaseBranch("releases/v1.0"), /Invalid branch format/);

// ----------------------------------------------------------
// deriveLatestRcMetadata tests
// ----------------------------------------------------------
const withRc = deriveLatestRcMetadata("cli/v0.3.1-rc.4", "cli");
assert.deepStrictEqual(withRc, {
  latestRcTag: "cli/v0.3.1-rc.4",
  latestRcVersion: "0.3.1-rc.4",
  latestPromotionVersion: "0.3.1",
  latestPromotionTag: "cli/v0.3.1",
});

const withoutRc = deriveLatestRcMetadata("", "cli");
assert.deepStrictEqual(withoutRc, {
  latestRcTag: "",
  latestRcVersion: "",
  latestPromotionVersion: "",
  latestPromotionTag: "",
});

const nestedComponent = deriveLatestRcMetadata("kubernetes/controller/v0.8.2-rc.1", "kubernetes/controller");
assert.deepStrictEqual(nestedComponent, {
  latestRcTag: "kubernetes/controller/v0.8.2-rc.1",
  latestRcVersion: "0.8.2-rc.1",
  latestPromotionVersion: "0.8.2",
  latestPromotionTag: "kubernetes/controller/v0.8.2",
});

console.log("✅ All resolve-latest-rc tests passed.");
