import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { evaluateKubescape, evaluateTrivy } from "./chart-benchmarks.js";

describe("evaluateKubescape", () => {
    const failed = (subStatus) => ({ status: "failed", ...(subStatus && { subStatus }) });
    const report = {
        summaryDetails: {
            controls: { "C-1": { name: "one", severity: "High" }, "C-2": { name: "two", severity: "Medium" } },
            frameworks: [{ name: "NSA", complianceScore: 92.5 }],
        },
        results: [
            { resourceID: "Deployment/a", controls: [{ controlID: "C-1", status: failed("w/exceptions") }, { controlID: "C-2", status: failed("w/exceptions") }] },
            // C-1 also fails on a resource without an exception
            { resourceID: "ServiceAccount/b", controls: [{ controlID: "C-1", status: failed() }, { controlID: "C-3", status: { status: "passed" } }] },
        ],
    };

    it("treats a control as open when any failing resource lacks an exception", () => {
        const result = evaluateKubescape(report);
        assert.deepEqual(result.open.map((c) => [c.id, c.open]), [["C-1", ["ServiceAccount/b"]]]);
        assert.deepEqual(result.accepted.map((c) => c.id), ["C-2"]);
        assert.equal(result.resources, 2);
    });

    it("reports nothing scanned for an empty or missing report", () => {
        assert.equal(evaluateKubescape({ results: [] }).resources, 0);
        assert.equal(evaluateKubescape(undefined).resources, 0);
    });
});

describe("evaluateTrivy", () => {
    it("counts checks across results and keeps only failures", () => {
        const report = {
            Results: [
                { Target: "a.yaml", MisconfSummary: { Successes: 10, Failures: 1 }, Misconfigurations: [{ ID: "KSV-1", Status: "FAIL", Severity: "HIGH" }, { ID: "KSV-2", Status: "PASS" }] },
                { Target: "b.yaml", MisconfSummary: { Successes: 5, Failures: 0 } },
            ],
        };
        const result = evaluateTrivy(report);
        assert.equal(result.checks, 16);
        assert.deepEqual(result.findings.map((f) => [f.id, f.target]), [["KSV-1", "a.yaml"]]);
    });

    it("reports no checks when Trivy detected no manifests", () => {
        assert.equal(evaluateTrivy({ ArtifactName: "chart", ArtifactType: "filesystem" }).checks, 0);
    });
});
