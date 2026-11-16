import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { addDockle, addKubescape, addStig, addTrivy, evaluate, loadModules, parseXccdfResults, ruleVerdict, stigCheck, validateModule } from "./bsi-report.js";

const repo = path.resolve(import.meta.dirname, "../..");

describe("control files", () => {
    it("are valid and reference only rules the OCM STIG supplement defines", async () => {
        const modules = await loadModules(path.join(repo, ".github/benchmarks/bsi"));
        assert.deepEqual(modules.map((m) => m.module), ["APP.4.4", "SYS.1.6"]);
        const xccdf = await readFile(path.join(repo, ".github/stig/ocm-supplement-xccdf.xml"), "utf8");
        const supplement = new Set([...xccdf.matchAll(/<Rule id="xccdf_software\.(ocm_rule_\w+)"/g)].map((m) => m[1]));
        for (const rule of modules.flatMap((m) => m.controls.flatMap((c) => c.rules ?? []))) {
            if (rule.startsWith("stig:ocm_")) assert.ok(supplement.has(rule.slice("stig:".length)), rule);
        }
    });
});

describe("validateModule", () => {
    const module = (control) => ({ module: "SYS.1.6", title: "t", edition: "2022", source: "s", controls: [{ id: "SYS.1.6.A1", title: "t", level: "basic", ocm: "o", ...control }] });

    it("accepts manual controls without rules and automated controls with rules", () => {
        assert.deepEqual(validateModule(module({ status: "manual" })), []);
        assert.deepEqual(validateModule(module({ status: "automated", rules: ["trivy:KSV-0012"] })), []);
    });

    it("rejects rules on manual controls and missing rules on checked controls", () => {
        assert.match(validateModule(module({ status: "manual", rules: ["trivy:KSV-0012"] })).join(), /manual controls have no rules/);
        assert.match(validateModule(module({ status: "partial" })).join(), /partial controls need rules/);
    });

    it("rejects unknown scanners and dockle checks it cannot count as passed", () => {
        assert.match(validateModule(module({ status: "automated", rules: ["kube-bench:1.1"] })).join(), /must be <stig\|dockle\|oci\|trivy\|kubescape>/);
        assert.match(validateModule(module({ status: "automated", rules: ["dockle:CIS-DI-0999"] })).join(), /unknown dockle check/);
    });

    it("keeps operator controls to the operator's part and manual controls to OCM measures", () => {
        assert.deepEqual(validateModule(module({ status: "operator", ocm: undefined, operator: "o" })), []);
        assert.match(validateModule(module({ status: "operator", operator: "o" })).join(), /operator controls describe only the operator's part/);
        assert.match(validateModule(module({ status: "manual", ocm: undefined, operator: "o" })).join(), /manual controls describe what OCM provides/);
    });

    it("rejects ids outside the module", () => {
        assert.match(validateModule(module({ status: "manual", id: "APP.4.4.A1" })).join(), /id must start with SYS\.1\.6\.A/);
    });
});

describe("stigCheck", () => {
    it("maps DISA rules to the SRG id and OCM rules to the bare id", () => {
        assert.equal(stigCheck("xccdf_mil.disa.stig_rule_SV-203716r982210_rule"), "SV-203716");
        assert.equal(stigCheck("xccdf_software.ocm_rule_minimal_rootfs"), "ocm_rule_minimal_rootfs");
    });
});

describe("ruleVerdict", () => {
    it("is missing when no scan evaluated the rule", () => {
        assert.equal(ruleVerdict(undefined).verdict, "missing");
        assert.equal(ruleVerdict([{ target: "cli", outcome: "skip" }]).verdict, "missing");
    });

    it("fails on any failing target and names it", () => {
        const v = ruleVerdict([{ target: "cli", outcome: "pass" }, { target: "controller", outcome: "fail" }, { target: "x", outcome: "accepted" }]);
        assert.deepEqual([v.verdict, v.failed], ["fail", ["controller"]]);
    });

    it("is accepted when an accepted finding is the only non-pass", () => {
        assert.equal(ruleVerdict([{ target: "a", outcome: "pass" }, { target: "b", outcome: "accepted" }]).verdict, "accepted");
    });
});

describe("addDockle", () => {
    it("counts unlisted checks as passed and only WARN and FATAL as failed", () => {
        const results = new Map();
        addDockle(results, { summary: {}, details: [{ code: "CIS-DI-0001", level: "WARN" }, { code: "CIS-DI-0005", level: "INFO" }] }, "cli");
        assert.deepEqual(results.get("dockle:CIS-DI-0001"), [{ target: "cli", outcome: "fail" }]);
        assert.deepEqual(results.get("dockle:CIS-DI-0005"), [{ target: "cli", outcome: "skip" }]);
        assert.deepEqual(results.get("dockle:CIS-DI-0008"), [{ target: "cli", outcome: "pass" }]);
    });

    it("records nothing for a report without a summary", () => {
        const results = new Map();
        addDockle(results, {}, "cli");
        assert.equal(results.size, 0);
    });
});

describe("addTrivy", () => {
    const full = {
        Results: [{
            Target: "chart/templates/rbac/manager-role.yaml",
            Misconfigurations: [{ ID: "KSV-0012", Status: "PASS" }, { ID: "KSV-0041", Status: "FAIL" }, { ID: "KSV-0043", Status: "FAIL" }],
        }],
    };

    it("accepts a finding the gated report no longer has and fails the rest", () => {
        const results = new Map();
        addTrivy(results, full, { Results: [{ Target: "chart/templates/rbac/manager-role.yaml", Misconfigurations: [{ ID: "KSV-0043", Status: "FAIL" }] }] });
        assert.equal(ruleVerdict(results.get("trivy:KSV-0012")).verdict, "pass");
        assert.equal(ruleVerdict(results.get("trivy:KSV-0041")).verdict, "accepted");
        assert.equal(ruleVerdict(results.get("trivy:KSV-0043")).verdict, "fail");
    });

    it("leaves a check the scan never reported missing, however it is ignored", () => {
        const results = new Map();
        // The ignore file still accepts KSV-0041, but a renamed or dropped
        // check produces no observation, so the rule must not count as accepted.
        addTrivy(results, { Results: [{ Target: "chart/templates/rbac/manager-role.yaml", Misconfigurations: [{ ID: "KSV-0012", Status: "PASS" }] }] }, { Results: [] });
        assert.equal(results.has("trivy:KSV-0041"), false);
        assert.equal(ruleVerdict(results.get("trivy:KSV-0041")).verdict, "missing");
    });
});

describe("addKubescape", () => {
    it("accepts failures covered by an exception and fails the open ones", () => {
        const results = new Map();
        addKubescape(results, {
            results: [{
                resourceID: "path=1/api//ns/Deployment/manager",
                controls: [
                    { controlID: "C-0007", status: { status: "failed", subStatus: "w/exceptions" } },
                    { controlID: "C-0015", status: { status: "failed" } },
                ],
            }],
            summaryDetails: { controls: { "C-0007": { status: "failed" }, "C-0015": { status: "failed" }, "C-0030": { status: "passed" }, "C-0054": { status: "skipped" } } },
        });
        assert.equal(ruleVerdict(results.get("kubescape:C-0007")).verdict, "accepted");
        assert.equal(ruleVerdict(results.get("kubescape:C-0015")).verdict, "fail");
        assert.equal(ruleVerdict(results.get("kubescape:C-0030")).verdict, "pass");
        assert.equal(ruleVerdict(results.get("kubescape:C-0054")).verdict, "missing");
    });
});

describe("addStig", () => {
    const xml = `<?xml version="1.0"?><cdf:TestResult xmlns:cdf="http://checklists.nist.gov/xccdf/1.2">
      <cdf:rule-result idref="xccdf_mil.disa.stig_rule_SV-203716r982210_rule"><cdf:result>pass</cdf:result></cdf:rule-result>
      <cdf:rule-result idref="xccdf_software.ocm_rule_minimal_rootfs"><cdf:result>fail</cdf:result></cdf:rule-result>
      <cdf:rule-result idref="xccdf_software.ocm_rule_ca_bundle"><cdf:result>notapplicable</cdf:result></cdf:rule-result>
      </cdf:TestResult>`;

    it("reads the rule results of an XCCDF document and maps them to outcomes", () => {
        assert.deepEqual(parseXccdfResults(xml).map((r) => r.result), ["pass", "fail", "notapplicable"]);
        const results = new Map();
        addStig(results, xml, "cli-amd64");
        assert.deepEqual(results.get("stig:SV-203716"), [{ target: "cli-amd64", outcome: "pass" }]);
        assert.deepEqual(results.get("stig:ocm_rule_minimal_rootfs"), [{ target: "cli-amd64", outcome: "fail" }]);
        assert.deepEqual(results.get("stig:ocm_rule_ca_bundle"), [{ target: "cli-amd64", outcome: "skip" }]);
    });
});

describe("evaluate", () => {
    it("fails a requirement whose rule no scan evaluated and keeps manual ones manual", () => {
        const results = new Map();
        addTrivy(
            results,
            { Results: [{ Target: "manager.yaml", Misconfigurations: [{ ID: "KSV-0012", Status: "PASS" }, { ID: "KSV-0041", Status: "FAIL" }] }] },
            { Results: [] },
        );
        const [module] = evaluate([{
            module: "SYS.1.6", title: "t", edition: "2022", source: "s",
            controls: [
                { id: "SYS.1.6.A1", title: "t", level: "basic", status: "manual" },
                { id: "SYS.1.6.A8", title: "t", level: "basic", status: "partial", rules: ["trivy:KSV-0041"] },
                { id: "SYS.1.6.A17", title: "t", level: "standard", status: "automated", rules: ["trivy:KSV-0012", "trivy:KSV-9999"] },
            ],
        }], results);
        assert.deepEqual(module.controls.map((c) => [c.id, c.result]), [["SYS.1.6.A1", "manual"], ["SYS.1.6.A8", "accepted"], ["SYS.1.6.A17", "fail"]]);
        assert.deepEqual(module.controls[2].rules.map((r) => [r.rule, r.verdict]), [["trivy:KSV-0012", "pass"], ["trivy:KSV-9999", "missing"]]);
    });
});
