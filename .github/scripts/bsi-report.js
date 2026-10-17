// @ts-check
import { readdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import * as yaml from "js-yaml";
import { evaluateKubescape } from "./chart-benchmarks.js";
import { parseXccdfResults } from "./stig-scan.js";

/**
 * Evaluates the BSI IT-Grundschutz requirements in .github/benchmarks/bsi
 * against the reports of the image-scan workflow, the way ComplianceAsCode
 * profiles map BSI controls to rules.
 *
 * Every rule of an automated or partial requirement must have been evaluated
 * by its scanner on at least one target. A rule that failed anywhere, or that
 * no scan evaluated (a renamed or dropped check), fails the report, so the
 * mapping cannot silently drift from what the scanners check.
 */

export const SCANNERS = /** @type {const} */ (["stig", "dockle", "oci", "trivy", "kubescape"]);
export const STATUSES = /** @type {const} */ (["automated", "partial", "manual", "operator"]);
/** Checks of image-metadata.js. */
export const OCI_CHECKS = new Set(["annotations", "provenance"]);
/** Statuses without rules: no automated check, or nothing OCM can influence. */
const UNCHECKED = new Set(["manual", "operator"]);
export const LEVELS = /** @type {const} */ (["basic", "standard", "elevated"]);

/**
 * dockle v0.4 checkpoints. dockle reports only the checks that did not pass,
 * so a mapped code must be one of these to be counted as passed when absent.
 */
export const DOCKLE_CHECKS = new Set([
    "CIS-DI-0001", "CIS-DI-0002", "CIS-DI-0003", "CIS-DI-0004", "CIS-DI-0005",
    "CIS-DI-0006", "CIS-DI-0007", "CIS-DI-0008", "CIS-DI-0009", "CIS-DI-0010",
    "DKL-DI-0001", "DKL-DI-0002", "DKL-DI-0003", "DKL-DI-0004", "DKL-DI-0005", "DKL-DI-0006",
    "DKL-LI-0001", "DKL-LI-0002", "DKL-LI-0003",
]);

/**
 * @typedef {"pass" | "accepted" | "fail" | "skip"} Outcome
 * @typedef {{ target: string, outcome: Outcome }} Observation
 * @typedef {Map<string, Observation[]>} Results - observations by rule reference ("<scanner>:<check>")
 * @typedef {{ id: string, title: string, level: string, status: string, gspp?: string[], ocm?: string, operator?: string, rules?: string[] }} Control
 * @typedef {{ module: string, title: string, edition: string, source: string, controls: Control[] }} Module
 */

/**
 * Structural problems of a control file.
 *
 * @param {any} module
 * @returns {string[]}
 */
export function validateModule(module) {
    const errors = [];
    for (const key of ["module", "title", "edition", "source"]) {
        if (typeof module?.[key] !== "string" || module[key] === "") errors.push(`missing ${key}`);
    }
    if (!Array.isArray(module?.controls) || module.controls.length === 0) return [...errors, "no controls"];
    const seen = new Set();
    for (const c of module.controls) {
        const where = c?.id ?? "control without id";
        if (typeof c?.id !== "string" || !c.id.startsWith(`${module.module}.A`)) errors.push(`${where}: id must start with ${module.module}.A`);
        if (seen.has(c?.id)) errors.push(`${where}: duplicate id`);
        seen.add(c?.id);
        if (typeof c?.title !== "string" || c.title === "") errors.push(`${where}: missing title`);
        if (!LEVELS.includes(c?.level)) errors.push(`${where}: level must be one of ${LEVELS.join(", ")}`);
        if (!STATUSES.includes(c?.status)) errors.push(`${where}: status must be one of ${STATUSES.join(", ")}`);
        if (c?.status === "operator" && (c?.ocm || !c?.operator)) errors.push(`${where}: operator controls describe only the operator's part`);
        else if (c?.status !== "operator" && !c?.ocm) errors.push(`${where}: ${c?.status} controls describe what OCM provides`);
        const rules = c?.rules ?? [];
        if (!Array.isArray(rules)) {
            errors.push(`${where}: rules must be a list`);
            continue;
        }
        if (UNCHECKED.has(c?.status) && rules.length > 0) errors.push(`${where}: ${c?.status} controls have no rules`);
        if (!UNCHECKED.has(c?.status) && rules.length === 0) errors.push(`${where}: ${c?.status} controls need rules`);
        if (new Set(rules).size !== rules.length) errors.push(`${where}: duplicate rule`);
        for (const rule of rules) {
            const [scanner, check] = String(rule).split(":");
            if (!SCANNERS.includes(/** @type {any} */ (scanner)) || !check) errors.push(`${where}: rule ${rule} must be <${SCANNERS.join("|")}>:<check>`);
            else if (scanner === "dockle" && !DOCKLE_CHECKS.has(check)) errors.push(`${where}: unknown dockle check ${check}`);
            else if (scanner === "oci" && !OCI_CHECKS.has(check)) errors.push(`${where}: unknown oci check ${check}`);
        }
    }
    return errors;
}

/**
 * Load all control files of a directory, sorted by module.
 *
 * @param {string} dir
 * @returns {Promise<Module[]>}
 */
export async function loadModules(dir) {
    const files = (await readdir(dir)).filter((f) => f.endsWith(".yaml")).sort();
    const modules = [];
    for (const file of files) {
        const module = /** @type {any} */ (yaml.load(await readFile(path.join(dir, file), "utf8")));
        const errors = validateModule(module);
        if (errors.length > 0) throw new Error(`${file}: ${errors.join("; ")}`);
        modules.push(module);
    }
    return modules;
}

/**
 * Rule reference of an XCCDF rule id: the SRG id for DISA rules
 * (xccdf_mil.disa.stig_rule_SV-203716r982210_rule -> SV-203716), the bare id
 * for OCM's supplement (xccdf_software.ocm_rule_minimal_rootfs -> ocm_rule_minimal_rootfs).
 *
 * @param {string} id
 */
export function stigCheck(id) {
    return id.match(/_rule_(SV-\d+)r\d+_rule$/)?.[1] ?? id.replace(/^xccdf_[^.]+\./, "");
}

/** @param {Results} results @param {string} rule @param {Observation} observation */
function observe(results, rule, observation) {
    const list = results.get(rule) ?? [];
    list.push(observation);
    results.set(rule, list);
}

/**
 * @param {Results} results
 * @param {string} xml - XCCDF results document
 * @param {string} target
 */
export function addStig(results, xml, target) {
    for (const r of parseXccdfResults(xml)) {
        /** @type {Outcome} */
        const outcome = r.result === "pass" ? "pass" : ["fail", "error", "unknown"].includes(r.result) ? "fail" : "skip";
        observe(results, `stig:${stigCheck(r.id)}`, { target, outcome });
    }
}

/**
 * dockle lists only checks that did not pass; WARN and FATAL fail the scan,
 * INFO and SKIP are not a verdict.
 *
 * @param {Results} results
 * @param {any} report - dockle JSON report
 * @param {string} target
 */
export function addDockle(results, report, target) {
    if (!report?.summary) return;
    /** @type {Map<string, string>} */
    const levels = new Map((report.details ?? []).map((/** @type {any} */ d) => [d.code, d.level]));
    for (const code of DOCKLE_CHECKS) {
        const level = levels.get(code);
        /** @type {Outcome} */
        const outcome = level === undefined ? "pass" : level === "WARN" || level === "FATAL" ? "fail" : "skip";
        observe(results, `dockle:${code}`, { target, outcome });
    }
}

/**
 * @param {Results} results
 * @param {{ id: string, result: string }[]} checks - image-metadata.js result
 * @param {string} target
 */
export function addOci(results, checks, target) {
    for (const c of checks) observe(results, `oci:${c.id}`, { target, outcome: c.result === "pass" ? "pass" : "fail" });
}

/**
 * Trivy config report run with --include-non-failures. Findings accepted in
 * the ignore file are dropped by Trivy, so they are added back as accepted.
 *
 * @param {Results} results
 * @param {any} report - Trivy JSON report
 * @param {any} ignore - parsed .github/benchmarks/trivyignore.yaml
 */
export function addTrivy(results, report, ignore) {
    for (const result of report?.Results ?? []) {
        for (const m of result.Misconfigurations ?? []) {
            /** @type {Outcome} */
            const outcome = m.Status === "PASS" ? "pass" : m.Status === "FAIL" ? "fail" : "skip";
            observe(results, `trivy:${m.ID}`, { target: result.Target, outcome });
        }
    }
    for (const m of ignore?.misconfigurations ?? []) {
        for (const target of m.paths ?? ["*"]) observe(results, `trivy:${m.id}`, { target, outcome: "accepted" });
    }
}

/**
 * @param {Results} results
 * @param {any} report - Kubescape JSON report
 */
export function addKubescape(results, report) {
    const { open, accepted } = evaluateKubescape(report);
    const openIds = new Set(open.map((c) => c.id));
    const acceptedIds = new Set(accepted.map((c) => c.id));
    for (const [id, control] of Object.entries(report?.summaryDetails?.controls ?? {})) {
        const status = /** @type {any} */ (control).status;
        /** @type {Outcome} */
        const outcome = openIds.has(id) ? "fail" : acceptedIds.has(id) ? "accepted" : status === "passed" ? "pass" : status === "failed" ? "fail" : "skip";
        observe(results, `kubescape:${id}`, { target: "chart", outcome });
    }
}

/**
 * Verdict of one rule over all its observations. "missing" means no scan
 * evaluated it.
 *
 * @param {Observation[] | undefined} observations
 * @returns {{ verdict: "pass" | "accepted" | "fail" | "missing", failed: string[], evaluated: number }}
 */
export function ruleVerdict(observations) {
    const evaluated = (observations ?? []).filter((o) => o.outcome !== "skip");
    const failed = evaluated.filter((o) => o.outcome === "fail").map((o) => o.target);
    if (evaluated.length === 0) return { verdict: "missing", failed, evaluated: 0 };
    if (failed.length > 0) return { verdict: "fail", failed, evaluated: evaluated.length };
    const accepted = evaluated.some((o) => o.outcome === "accepted");
    return { verdict: accepted ? "accepted" : "pass", failed, evaluated: evaluated.length };
}

/**
 * @param {Module[]} modules
 * @param {Results} results
 */
export function evaluate(modules, results) {
    return modules.map((module) => ({
        module: module.module,
        title: module.title,
        edition: module.edition,
        controls: module.controls.map((control) => {
            const rules = (control.rules ?? []).map((rule) => ({ rule, ...ruleVerdict(results.get(rule)) }));
            const verdicts = new Set(rules.map((r) => r.verdict));
            const result = UNCHECKED.has(control.status) ? control.status
                : verdicts.has("fail") || verdicts.has("missing") ? "fail"
                : verdicts.has("accepted") ? "accepted" : "pass";
            return { id: control.id, title: control.title, level: control.level, status: control.status, gspp: control.gspp ?? [], result, rules };
        }),
    }));
}

/** @param {string} file */
async function readOptional(file) {
    try {
        return await readFile(file, "utf8");
    } catch (error) {
        if (/** @type {any} */ (error)?.code === "ENOENT") return undefined;
        throw error;
    }
}

/**
 * Collect the results of a directory holding the downloaded scan artifacts:
 * image-scan-<image>-<arch>/{ocm,gpos}-results.xml, dockle.json and metadata.json, and
 * chart-scan/{trivy,kubescape}.json.
 *
 * @param {string} reportsDir
 * @param {any} trivyIgnore
 * @returns {Promise<{ results: Results, errors: string[] }>}
 */
export async function collect(reportsDir, trivyIgnore) {
    /** @type {Results} */
    const results = new Map();
    const errors = [];
    const images = (await readdir(reportsDir)).filter((d) => d.startsWith("image-scan-")).sort();
    if (images.length === 0) errors.push(`no image-scan-* reports in ${reportsDir}`);
    for (const dir of images) {
        const target = dir.slice("image-scan-".length);
        for (const file of ["ocm-results.xml", "gpos-results.xml"]) {
            const xml = await readOptional(path.join(reportsDir, dir, file));
            if (xml === undefined) errors.push(`${dir}: missing ${file}`);
            else addStig(results, xml, target);
        }
        const dockle = await readOptional(path.join(reportsDir, dir, "dockle.json"));
        if (dockle === undefined) errors.push(`${dir}: missing dockle.json`);
        else addDockle(results, JSON.parse(dockle), target);
        const metadata = await readOptional(path.join(reportsDir, dir, "metadata.json"));
        if (metadata === undefined) errors.push(`${dir}: missing metadata.json`);
        else addOci(results, JSON.parse(metadata), target);
    }
    const trivy = await readOptional(path.join(reportsDir, "chart-scan", "trivy.json"));
    if (trivy === undefined) errors.push("chart-scan: missing trivy.json");
    else addTrivy(results, JSON.parse(trivy), trivyIgnore);
    const kubescape = await readOptional(path.join(reportsDir, "chart-scan", "kubescape.json"));
    if (kubescape === undefined) errors.push("chart-scan: missing kubescape.json");
    else addKubescape(results, JSON.parse(kubescape));
    return { results, errors };
}

const ICON = { pass: "✅", accepted: "☑️", fail: "❌", missing: "❓", manual: "📝", operator: "👤" };

/**
 * GitHub Actions entrypoint.
 *
 * Environment variables:
 * - REPORTS_DIR: directory with the downloaded scan artifacts (required)
 * - CONTROLS_DIR: directory with the control files (default .github/benchmarks/bsi)
 * - TRIVY_IGNORE: Trivy ignore file (default .github/benchmarks/trivyignore.yaml)
 * - OUTPUT: path of the JSON result (optional)
 *
 * @param {import('@actions/github-script').AsyncFunctionArguments} args
 */
export default async function bsiReportAction({ core }) {
    const {
        REPORTS_DIR: reportsDir,
        CONTROLS_DIR: controlsDir = ".github/benchmarks/bsi",
        TRIVY_IGNORE: trivyIgnorePath = ".github/benchmarks/trivyignore.yaml",
        OUTPUT: output,
    } = process.env;
    if (!reportsDir) {
        core.setFailed("REPORTS_DIR environment variable is required");
        return;
    }
    const modules = await loadModules(controlsDir);
    const { results, errors } = await collect(reportsDir, yaml.load(await readFile(trivyIgnorePath, "utf8")));
    const report = evaluate(modules, results);
    if (output) await writeFile(output, `${JSON.stringify(report, null, 2)}\n`);

    const problems = [...errors];
    for (const module of report) {
        for (const control of module.controls) {
            for (const r of control.rules) {
                if (r.verdict === "fail") problems.push(`${control.id}: ${r.rule} failed on ${r.failed.join(", ")}`);
                if (r.verdict === "missing") problems.push(`${control.id}: ${r.rule} was not evaluated by any scan`);
            }
        }
    }
    for (const p of problems) core.error(p);

    if (process.env.GITHUB_STEP_SUMMARY) {
        const summary = core.summary.addHeading("🛡️ BSI IT-Grundschutz");
        summary.addRaw("Status describes the part OCM can influence: automated and partial requirements are checked by the listed rules; manual ones are documented only; operator ones are entirely the operator's responsibility. ☑️ = accepted finding, see .github/benchmarks.", true);
        for (const module of report) {
            summary.addHeading(`${module.module} ${module.title} (Edition ${module.edition})`, 3);
            summary.addTable([
                ["Requirement", "Level", "Status", "Result", "Rules"].map((data) => ({ data, header: true })),
                ...module.controls.map((c) => [
                    `${c.id} ${c.title}`,
                    c.level,
                    c.status,
                    `${ICON[/** @type {keyof typeof ICON} */ (c.result)]} ${c.result}`,
                    c.rules.map((r) => `${ICON[r.verdict]} ${r.rule}`).join("<br>"),
                ]),
            ]);
        }
        if (problems.length > 0) summary.addHeading("Errors", 3).addList(problems);
        await summary.write();
    }

    for (const module of report) {
        const counts = Object.groupBy(module.controls, (c) => c.result);
        core.info(`${module.module}: ${Object.entries(counts).map(([k, v]) => `${v?.length} ${k}`).join(", ")}`);
    }
    if (problems.length > 0) core.setFailed(`${problems.length} BSI rule problem(s)`);
}
