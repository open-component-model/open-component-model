// @ts-check
import { readFile } from "node:fs/promises";
import path from "node:path";

/**
 * STIG scan summary for the GitHub Actions job summary.
 * Reads the XCCDF results that .github/scripts/stig-scan.sh writes.
 */

/**
 * @typedef {object} RuleResult
 * @property {string} id - XCCDF rule id
 * @property {string} title - rule title
 * @property {string} result - XCCDF result (pass, fail, notapplicable, ...)
 * @property {string[]} references - referenced SRG rules (SV-...)
 */

const FAILING = new Set(["fail", "error", "unknown"]);

/**
 * Parse an XCCDF results document into per-rule results. Element names may
 * carry a namespace prefix.
 *
 * @param {string} xml - XCCDF results document
 * @returns {RuleResult[]}
 */
export function parseXccdfResults(xml) {
    /** @type {Map<string, {title: string, references: string[]}>} */
    const rules = new Map();
    for (const block of xml.split(/<(?:\w+:)?Rule\b/).slice(1)) {
        const id = block.match(/^[^>]*\bid="([^"]+)"/)?.[1];
        if (!id) continue;
        const body = block.split(/<\/(?:\w+:)?Rule>/)[0];
        const title = body.match(/<(?:\w+:)?title\b[^>]*>([^<]*)</)?.[1]?.trim() ?? "";
        const references = [...body.matchAll(/<(?:\w+:)?reference\b[^>]*>(SV-\d+)[^<]*</g)].map((m) => m[1]);
        rules.set(id, { title, references });
    }

    /** @type {RuleResult[]} */
    const results = [];
    for (const m of xml.matchAll(/<(?:\w+:)?rule-result\b[^>]*\bidref="([^"]+)"[^>]*>[\s\S]*?<(?:\w+:)?result>([a-z]+)</g)) {
        const rule = rules.get(m[1]);
        results.push({ id: m[1], title: rule?.title ?? "", result: m[2], references: rule?.references ?? [] });
    }
    return results;
}

/**
 * Count results per XCCDF result value.
 *
 * @param {RuleResult[]} results
 * @returns {Record<string, number>}
 */
export function countResults(results) {
    /** @type {Record<string, number>} */
    const counts = {};
    for (const r of results) counts[r.result] = (counts[r.result] ?? 0) + 1;
    return counts;
}

/**
 * GitHub Actions entrypoint: writes the STIG scan summary.
 *
 * Environment variables:
 * - RESULTS_DIR: output directory of stig-scan.sh (required)
 * - IMAGE: image name for the heading, e.g. "cli" (required)
 * - ARCH: image architecture, e.g. "arm64" (required)
 *
 * @param {import('@actions/github-script').AsyncFunctionArguments} args
 */
export default async function stigSummaryAction({ core }) {
    const dir = process.env.RESULTS_DIR;
    const image = process.env.IMAGE;
    const arch = process.env.ARCH;
    if (!dir || !image || !arch) {
        core.setFailed("RESULTS_DIR, IMAGE and ARCH environment variables are required");
        return;
    }

    /** @type {Record<string, RuleResult[]>} */
    const evaluations = {};
    for (const [name, file] of [["GPOS SRG (tailored Chainguard profile)", "gpos-results.xml"], ["OCM supplement", "ocm-results.xml"]]) {
        try {
            evaluations[name] = parseXccdfResults(await readFile(path.join(dir, file), "utf8"));
        } catch {
            core.warning(`STIG results ${file} missing in ${dir}`);
        }
    }

    const columns = ["pass", "fail", "error", "notapplicable", "notselected"];
    const summary = core.summary
        .addHeading(`🛡️ STIG Scan: ${image} (linux/${arch})`)
        .addTable([
            [{ data: "Evaluation", header: true }, ...columns.map((c) => ({ data: c, header: true }))],
            ...Object.entries(evaluations).map(([name, results]) => {
                const counts = countResults(results);
                return [name, ...columns.map((c) => String(counts[c] ?? 0))];
            }),
        ]);

    const supplement = evaluations["OCM supplement"] ?? [];
    if (supplement.length > 0) {
        summary.addHeading("OCM checks", 3).addTable([
            [{ data: "Check", header: true }, { data: "SRG rules", header: true }, { data: "Result", header: true }],
            ...supplement.map((r) => [r.title, r.references.join(", "), FAILING.has(r.result) ? `❌ ${r.result}` : `✅ ${r.result}`]),
        ]);
    }

    const failed = Object.values(evaluations).flat().filter((r) => FAILING.has(r.result));
    if (failed.length > 0) {
        summary.addHeading("Failed rules", 3).addList(failed.map((r) => `${r.title || r.id} (${r.id}): ${r.result}`));
    }
    if (Object.keys(evaluations).length < 2) {
        summary.addRaw("⚠️ The scan did not produce all results; see the job log.", true);
    }
    await summary.write();
}
