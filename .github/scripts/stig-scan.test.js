import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { countResults, executablesPattern, failedOvalTests, goBuildSettings, parseXccdfResults } from "./stig-scan.js";

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

describe("goBuildSettings", () => {
    it("reads build settings from binary data", () => {
        const binary = Buffer.concat([
            Buffer.from([0x7f, 0x45, 0x4c, 0x46, 0, 0, 0]),
            Buffer.from("\0\0path\tocm.software/cli\nmod\tocm.software/cli\t(devel)\t\nbuild\tCGO_ENABLED=0\nbuild\tDefaultGODEBUG=fips140=on,tlssha1=1\nbuild\tGOFIPS140=v1.0.0-c2097c7c\n\0\0"),
        ]);
        assert.deepEqual(goBuildSettings(binary), {
            CGO_ENABLED: "0",
            DefaultGODEBUG: "fips140=on,tlssha1=1",
            GOFIPS140: "v1.0.0-c2097c7c",
        });
    });

    it("ignores lines that only contain the setting text", () => {
        assert.deepEqual(goBuildSettings(Buffer.from("xbuild\tGOFIPS140=v1\n")), {});
    });
});

describe("failedOvalTests", () => {
    it("names failed, errored and unknown tests by comment", () => {
        const definitions = `<unix:file_test id="oval:t:tst:1" version="1" check="all" comment="setuid files">
<ind:variable_test id="oval:t:tst:2" version="1" comment="GOFIPS140=v...">
<unix:file_test id="oval:t:tst:3" version="1" comment="log files">`;
        const results = `<test test_id="oval:t:tst:1" version="1" check="all" result="false"/>
<test test_id="oval:t:tst:2" version="1" result="true"/>
<test test_id="oval:t:tst:3" version="1" result="error"/>
<test test_id="oval:t:tst:9" version="1" result="unknown"/>`;
        assert.deepEqual(failedOvalTests(results, definitions), ["setuid files", "log files", "oval:t:tst:9"]);
    });
});

describe("executablesPattern", () => {
    it("matches exactly the given paths", () => {
        const re = new RegExp(executablesPattern(["/ocm", "/usr/bin/gpg-agent", "/usr/bin/gpg", "/ocm"]));
        for (const p of ["/ocm", "/usr/bin/gpg", "/usr/bin/gpg-agent"]) assert.ok(re.test(p), p);
        // "." must not match any character, and paths must not match as prefixes or suffixes.
        for (const p of ["/ocmx", "/x/ocm", "/usr/bin/gpgconf", "/usr/bin/gpg-agentx"]) assert.ok(!re.test(p), p);
        assert.ok(!new RegExp(executablesPattern(["/a.b"])).test("/axb"));
    });

    it("rejects relative and missing paths", () => {
        assert.throws(() => executablesPattern(["/ocm", "usr/bin/gpg"]));
        assert.throws(() => executablesPattern([]));
    });
});
