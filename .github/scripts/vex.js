// @ts-check
import { readFile, writeFile } from "node:fs/promises";

/**
 * Builds an OpenVEX document from govulncheck's source-mode JSON output.
 *
 * govulncheck reports, per advisory, whether the vulnerable code is required
 * (module), imported (package) or called (function). Advisories that are not
 * called get a `not_affected` statement, called ones `affected`. Products are
 * the vulnerable modules' package URLs, so image scanners (Trivy) that find the
 * same module in the binary's build information apply the statement instead of
 * reporting a finding that govulncheck already ruled out.
 */

/**
 * Parse govulncheck's JSON output: a stream of concatenated JSON objects.
 *
 * @param {string} text
 * @returns {any[]}
 */
export function parseJsonStream(text) {
    const objects = [];
    let depth = 0;
    let start = -1;
    let inString = false;
    let escaped = false;
    for (let i = 0; i < text.length; i++) {
        const c = text[i];
        if (inString) {
            if (escaped) escaped = false;
            else if (c === "\\") escaped = true;
            else if (c === '"') inString = false;
            continue;
        }
        if (c === '"') inString = true;
        else if (c === "{") {
            if (depth++ === 0) start = i;
        } else if (c === "}" && --depth === 0) {
            objects.push(JSON.parse(text.slice(start, i + 1)));
        }
    }
    return objects;
}

/**
 * @param {any[]} messages - parsed govulncheck JSON messages
 * @param {{id: string, author: string, timestamp: string}} meta
 */
export function buildVex(messages, meta) {
    /** @type {Map<string, {called: boolean, imported: boolean, products: Set<string>}>} */
    const byOsv = new Map();
    for (const { finding } of messages) {
        if (!finding?.trace?.length) continue;
        const entry = byOsv.get(finding.osv) ?? { called: false, imported: false, products: new Set() };
        const frame = finding.trace[0];
        if (frame.function) entry.called = true;
        if (frame.package) entry.imported = true;
        if (frame.module && frame.version) entry.products.add(`pkg:golang/${frame.module}@${frame.version}`);
        byOsv.set(finding.osv, entry);
    }

    const statements = [...byOsv].map(([osv, e]) => ({
        vulnerability: { "@id": `https://pkg.go.dev/vuln/${osv}`, name: osv },
        products: [...e.products].sort().map((id) => ({ "@id": id })),
        ...(e.called
            ? { status: "affected", action_statement: "The vulnerable code is called; see the govulncheck report." }
            : {
                  status: "not_affected",
                  justification: e.imported ? "vulnerable_code_not_in_execute_path" : "vulnerable_code_not_present",
                  impact_statement: e.imported
                      ? "govulncheck: the vulnerable package is imported, but no vulnerable symbol is called."
                      : "govulncheck: the vulnerable package is not imported; only the module is required.",
              }),
    }));

    return {
        "@context": "https://openvex.dev/ns/v0.2.0",
        "@id": meta.id,
        author: meta.author,
        timestamp: meta.timestamp,
        version: 1,
        tooling: "govulncheck (source mode), .github/scripts/vex.js",
        statements,
    };
}

/**
 * GitHub Actions entrypoint.
 *
 * Environment variables:
 * - GOVULNCHECK_JSON: govulncheck -format json output (required)
 * - VEX_OUTPUT: path of the OpenVEX document to write (required)
 *
 * @param {import('@actions/github-script').AsyncFunctionArguments} args
 */
export default async function vexAction({ core, context }) {
    const input = process.env.GOVULNCHECK_JSON;
    const output = process.env.VEX_OUTPUT;
    if (!input || !output) {
        core.setFailed("GOVULNCHECK_JSON and VEX_OUTPUT environment variables are required");
        return;
    }
    const vex = buildVex(parseJsonStream(await readFile(input, "utf8")), {
        id: `${context.serverUrl}/${context.repo.owner}/${context.repo.repo}/actions/runs/${context.runId}/vex`,
        author: `${context.repo.owner}/${context.repo.repo}`,
        timestamp: new Date().toISOString(),
    });
    await writeFile(output, JSON.stringify(vex, null, 2));
    const affected = vex.statements.filter((s) => s.status === "affected");
    core.info(`OpenVEX: ${vex.statements.length} statement(s), ${affected.length} affected`);
    await core.summary
        .addHeading("📄 OpenVEX from govulncheck", 3)
        .addTable([
            [{ data: "Advisory", header: true }, { data: "Status", header: true }, { data: "Products", header: true }],
            ...vex.statements.map((s) => [s.vulnerability.name, s.status + (s.justification ? ` (${s.justification})` : ""), s.products.map((p) => p["@id"]).join(", ")]),
        ])
        .write();
}
