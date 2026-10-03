import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { buildVex, parseJsonStream } from "./vex.js";

const meta = { id: "urn:test", author: "test", timestamp: "2026-10-03T00:00:00Z" };

describe("parseJsonStream", () => {
    it("splits concatenated objects, ignoring braces inside strings", () => {
        const text = '{"config":{"a":"}{"}}\n{\n  "finding": {"osv": "GO-1", "trace": []}\n}';
        assert.deepEqual(parseJsonStream(text), [{ config: { a: "}{" } }, { finding: { osv: "GO-1", trace: [] } }]);
    });
});

describe("buildVex", () => {
    it("derives status and justification from the deepest trace level per advisory", () => {
        const messages = [
            { config: {} },
            { finding: { osv: "GO-MOD", trace: [{ module: "golang.org/x/crypto", version: "v0.57.0" }] } },
            { finding: { osv: "GO-PKG", trace: [{ module: "example.com/a", version: "v1.0.0", package: "example.com/a/p" }] } },
            { finding: { osv: "GO-CALL", trace: [{ module: "example.com/b", version: "v2.0.0", package: "example.com/b", function: "F" }] } },
            // module- and symbol-level findings for the same advisory: called wins
            { finding: { osv: "GO-CALL", trace: [{ module: "example.com/b", version: "v2.0.0" }] } },
        ];
        const vex = buildVex(messages, meta);
        assert.equal(vex["@context"], "https://openvex.dev/ns/v0.2.0");
        const byName = Object.fromEntries(vex.statements.map((s) => [s.vulnerability.name, s]));
        assert.equal(byName["GO-MOD"].status, "not_affected");
        assert.equal(byName["GO-MOD"].justification, "vulnerable_code_not_present");
        assert.deepEqual(byName["GO-MOD"].products, [{ "@id": "pkg:golang/golang.org/x/crypto@v0.57.0" }]);
        assert.equal(byName["GO-PKG"].justification, "vulnerable_code_not_in_execute_path");
        assert.equal(byName["GO-CALL"].status, "affected");
        assert.equal(byName["GO-CALL"].justification, undefined);
    });

    it("has no statements without findings", () => {
        assert.deepEqual(buildVex([{ config: {} }], meta).statements, []);
    });
});
