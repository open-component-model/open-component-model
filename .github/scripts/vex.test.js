import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { buildVex, importAttested, normalizeGoPurl, parseJsonStream } from "./vex.js";

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
            { SBOM: { modules: [{ path: "example.com/main" }, { path: "golang.org/x/crypto", version: "v0.57.0" }] } },
            { osv: { id: "GO-CALL", aliases: ["GHSA-x", "CVE-2"] } },
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
        // scoped to the main module, so the statement cannot apply to the same
        // module in another binary of an image
        assert.deepEqual(byName["GO-MOD"].products, [{ "@id": "pkg:golang/example.com/main", subcomponents: [{ "@id": "pkg:golang/golang.org/x/crypto@v0.57.0" }] }]);
        // aliases are what image scanners report (CVE IDs)
        assert.deepEqual(byName["GO-CALL"].vulnerability.aliases, ["CVE-2", "GHSA-x"]);
        assert.deepEqual(byName["GO-MOD"].vulnerability.aliases, []);
        assert.equal(byName["GO-PKG"].justification, "vulnerable_code_not_in_execute_path");
        assert.equal(byName["GO-CALL"].status, "affected");
        assert.equal(byName["GO-CALL"].justification, undefined);
    });

    it("has no statements without findings", () => {
        assert.deepEqual(buildVex([{ config: {} }, { SBOM: { modules: [{ path: "example.com/main" }] } }], meta).statements, []);
    });

    it("refuses output that names no main module", () => {
        assert.throws(() => buildVex([{ config: {} }], meta), /no main module/);
    });
});

describe("normalizeGoPurl", () => {
    it("decodes the module path govulncheck percent-encodes, keeping version and qualifiers", () => {
        assert.equal(normalizeGoPurl("pkg:golang/golang.org%2Fx%2Fcrypto@v0.58.0"), "pkg:golang/golang.org/x/crypto@v0.58.0");
        assert.equal(normalizeGoPurl("pkg:golang/stdlib@v1.27.2?arch=amd64"), "pkg:golang/stdlib@v1.27.2?arch=amd64");
        assert.equal(normalizeGoPurl("pkg:oci/cosign"), "pkg:oci/cosign");
    });
});

describe("importAttested", () => {
    // govulncheck -format openvex output as the cosign release attests it
    const verified = [{
        verificationResult: {
            signature: { certificate: { buildSignerURI: "https://github.com/o/cosign/.github/workflows/release.yml@refs/heads/x" } },
            statement: {
                predicate: {
                    statements: [
                        { vulnerability: { name: "GO-1" }, products: [{ "@id": "Unknown Product", subcomponents: [{ "@id": "pkg:golang/golang.org%2Fx%2Fcrypto@v0.58.0" }] }], status: "not_affected", justification: "vulnerable_code_not_present" },
                        { vulnerability: { name: "GO-2" }, products: [{ "@id": "Unknown Product" }], status: "not_affected", justification: "vulnerable_code_not_present" },
                    ],
                },
            },
        },
    }];
    const target = { product: "pkg:golang/github.com/sigstore/cosign/v3", subject: "ghcr.io/o/cosign@sha256:abc" };

    it("scopes statements to the binary's main module with scanner-style purls and aliases", async () => {
        const { statements } = await importAttested(verified, target, async (id) => (id === "GO-1" ? ["CVE-1"] : []));
        assert.equal(statements.length, 1);
        const [s] = statements;
        assert.deepEqual(s.vulnerability, { "@id": "https://pkg.go.dev/vuln/GO-1", name: "GO-1", aliases: ["CVE-1"] });
        assert.deepEqual(s.products, [{ "@id": target.product, subcomponents: [{ "@id": "pkg:golang/golang.org/x/crypto@v0.58.0" }] }]);
        assert.deepEqual([s.status, s.justification], ["not_affected", "vulnerable_code_not_present"]);
        assert.match(s.status_notes, /ghcr\.io\/o\/cosign@sha256:abc, signed by https:\/\/github\.com\/o\/cosign/);
    });

    it("skips statements that name no module instead of covering the whole binary", async () => {
        const { skipped } = await importAttested(verified, target, async () => []);
        assert.deepEqual(skipped, ["GO-2"]);
    });
});
