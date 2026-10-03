// @ts-check
import { readFile } from "node:fs/promises";

/**
 * Gate for the benchmark scans of the rendered controller Helm chart
 * (image-scan.yml, job "chart"): Trivy `config` (CIS Kubernetes Benchmark and
 * Trivy's Kubernetes checks) and Kubescape (NSA/CISA and MITRE frameworks).
 *
 * Both scanners exit 0 when they find nothing to scan, and Kubescape keeps
 * reporting a control as failed even when every failing resource is covered by
 * an exception. This gate fails when either scanner checked nothing, on any
 * Trivy finding (accepted ones are already dropped via the ignore file), and on
 * any Kubescape control that fails on a resource without an exception.
 */

/**
 * @param {any} report - Trivy JSON report
 */
export function evaluateTrivy(report) {
    let checks = 0;
    const findings = [];
    for (const result of report?.Results ?? []) {
        checks += (result.MisconfSummary?.Successes ?? 0) + (result.MisconfSummary?.Failures ?? 0);
        for (const m of result.Misconfigurations ?? []) {
            if (m.Status === "FAIL") findings.push({ id: m.ID, severity: m.Severity, title: m.Title, target: result.Target, message: m.Message });
        }
    }
    return { checks, findings };
}

/**
 * @param {any} report - Kubescape JSON report
 */
export function evaluateKubescape(report) {
    const controls = report?.summaryDetails?.controls ?? {};
    /** @type {Map<string, {id: string, name: string, severity: string, open: string[], accepted: string[]}>} */
    const failed = new Map();
    let resources = 0;
    for (const result of report?.results ?? []) {
        resources++;
        for (const c of result.controls ?? []) {
            if (c.status?.status !== "failed") continue;
            const meta = controls[c.controlID] ?? {};
            const entry = failed.get(c.controlID) ?? { id: c.controlID, name: meta.name ?? c.name ?? "", severity: meta.severity ?? "", open: [], accepted: [] };
            (c.status.subStatus === "w/exceptions" ? entry.accepted : entry.open).push(result.resourceID);
            failed.set(c.controlID, entry);
        }
    }
    const frameworks = (report?.summaryDetails?.frameworks ?? []).map((/** @type {any} */ f) => ({ name: f.name, score: f.complianceScore }));
    const all = [...failed.values()].sort((a, b) => a.id.localeCompare(b.id));
    return { resources, frameworks, open: all.filter((c) => c.open.length > 0), accepted: all.filter((c) => c.open.length === 0) };
}

/**
 * GitHub Actions entrypoint.
 *
 * Environment variables:
 * - TRIVY_JSON: Trivy `config` JSON report (required)
 * - KUBESCAPE_JSON: Kubescape JSON report (required)
 * - SUMMARY_TITLE: heading of the job summary; no summary is written without it
 *
 * @param {import('@actions/github-script').AsyncFunctionArguments} args
 */
export default async function chartBenchmarksAction({ core }) {
    const { TRIVY_JSON: trivyPath, KUBESCAPE_JSON: kubescapePath, SUMMARY_TITLE: title } = process.env;
    if (!trivyPath || !kubescapePath) {
        core.setFailed("TRIVY_JSON and KUBESCAPE_JSON environment variables are required");
        return;
    }

    /** @type {string[]} */
    const errors = [];
    /** @param {string} file */
    const load = async (file) => {
        try {
            return JSON.parse(await readFile(file, "utf8"));
        } catch (error) {
            errors.push(`cannot read ${file}: ${error instanceof Error ? error.message : error}`);
            return undefined;
        }
    };
    const trivy = evaluateTrivy(await load(trivyPath));
    const kubescape = evaluateKubescape(await load(kubescapePath));
    if (trivy.checks === 0) errors.push("Trivy checked no Kubernetes manifests");
    if (kubescape.resources === 0) errors.push("Kubescape scanned no Kubernetes resources");

    core.info(`Trivy: ${trivy.checks} checks, ${trivy.findings.length} finding(s)`);
    core.info(`Kubescape: ${kubescape.resources} resources, ${kubescape.frameworks.map((f) => `${f.name} ${f.score.toFixed(2)}%`).join(", ")}`);
    for (const f of trivy.findings) core.error(`Trivy ${f.id} (${f.severity}) in ${f.target}: ${f.message}`);
    for (const c of kubescape.open) core.error(`Kubescape ${c.id} ${c.name} (${c.severity}) fails on: ${c.open.join(", ")}`);

    if (title && process.env.GITHUB_STEP_SUMMARY) {
        const summary = core.summary.addHeading(`📏 Benchmarks: ${title}`);
        summary.addHeading("CIS Kubernetes Benchmark (Trivy config)", 3)
            .addRaw(`${trivy.checks} checks, ${trivy.findings.length} open finding(s); accepted findings: .github/benchmarks/trivyignore.yaml`, true);
        if (trivy.findings.length > 0) {
            summary.addTable([
                [{ data: "Check", header: true }, { data: "Severity", header: true }, { data: "Template", header: true }, { data: "Message", header: true }],
                ...trivy.findings.map((f) => [f.id, f.severity, f.target, f.message]),
            ]);
        }
        summary.addHeading("NSA/CISA and MITRE (Kubescape)", 3)
            .addRaw(`${kubescape.resources} resources; ${kubescape.frameworks.map((f) => `${f.name} ${f.score.toFixed(2)}%`).join(", ")}`, true);
        const rows = [...kubescape.open.map((c) => [c.id, c.name, c.severity, "❌ open"]), ...kubescape.accepted.map((c) => [c.id, c.name, c.severity, "☑️ accepted"])];
        if (rows.length > 0) {
            summary.addTable([[{ data: "Control", header: true }, { data: "Name", header: true }, { data: "Severity", header: true }, { data: "Status", header: true }], ...rows]);
        }
        if (errors.length > 0) summary.addHeading("Errors", 3).addList(errors);
        await summary.write();
    }

    const problems = [...errors];
    if (trivy.findings.length > 0) problems.push(`${trivy.findings.length} Trivy finding(s)`);
    if (kubescape.open.length > 0) problems.push(`${kubescape.open.length} Kubescape control(s) failed without an exception`);
    if (problems.length > 0) core.setFailed(problems.join("; "));
}
