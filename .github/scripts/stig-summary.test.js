import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { countResults, parseXccdfResults } from "./stig-summary.js";

// Shape of oscap's --results output: the benchmark (rules) followed by the TestResult.
const results = (prefix) => `<?xml version="1.0" encoding="UTF-8"?>
<${prefix}Benchmark xmlns${prefix ? `:${prefix.slice(0, -1)}` : ""}="http://checklists.nist.gov/xccdf/1.2" id="b">
  <${prefix}Value id="v"><${prefix}title xml:lang="en">A value, not a rule</${prefix}title></${prefix}Value>
  <${prefix}Rule id="xccdf_r_one" selected="true" severity="high">
    <${prefix}title xmlns:xhtml="http://www.w3.org/1999/xhtml" xml:lang="en">First rule</${prefix}title>
    <${prefix}reference href="https://public.cyber.mil/stigs/">SV-203649</${prefix}reference>
    <${prefix}reference href="https://public.cyber.mil/stigs/">SV-203739</${prefix}reference>
  </${prefix}Rule>
  <${prefix}Rule id="xccdf_r_two" selected="true">
    <${prefix}title xml:lang="en">Second rule</${prefix}title>
  </${prefix}Rule>
  <${prefix}TestResult id="t">
    <${prefix}rule-result idref="xccdf_r_one" role="full" time="2026-10-03T12:00:00+00:00" severity="high" weight="1.000000">
      <${prefix}result>pass</${prefix}result>
      <${prefix}check system="http://oval.mitre.org/XMLSchema/oval-definitions-5"/>
    </${prefix}rule-result>
    <${prefix}rule-result idref="xccdf_r_two" role="full" time="2026-10-03T12:00:00+00:00">
      <${prefix}result>fail</${prefix}result>
    </${prefix}rule-result>
    <${prefix}rule-result idref="xccdf_r_unknown" role="full">
      <${prefix}result>notselected</${prefix}result>
    </${prefix}rule-result>
  </${prefix}TestResult>
</${prefix}Benchmark>`;

describe("parseXccdfResults", () => {
    for (const prefix of ["", "ns0:"]) {
        it(`maps rule results to titles and SRG references (prefix "${prefix}")`, () => {
            assert.deepEqual(parseXccdfResults(results(prefix)), [
                { id: "xccdf_r_one", title: "First rule", result: "pass", references: ["SV-203649", "SV-203739"] },
                { id: "xccdf_r_two", title: "Second rule", result: "fail", references: [] },
                { id: "xccdf_r_unknown", title: "", result: "notselected", references: [] },
            ]);
        });
    }

    it("counts results per value", () => {
        assert.deepEqual(countResults(parseXccdfResults(results(""))), { pass: 1, fail: 1, notselected: 1 });
    });
});
