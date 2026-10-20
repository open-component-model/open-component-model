// @ts-check
import { readFile, writeFile } from "node:fs/promises";

/**
 * Builds an OpenVEX document from govulncheck's source-mode JSON output.
 *
 * govulncheck reports, per advisory, whether the vulnerable code is required
 * (module), imported (package) or called (function). Advisories that are not
 * called get a `not_affected` statement, called ones `affected`.
 *
 * govulncheck's verdicts hold for the build configuration it analyzed (GOOS,
 * GOARCH, build tags), so a document applies to the images of that one platform.
 *
 * The product of every statement is the scanned main module (the binaries built
 * from it: ocm, manager), with the vulnerable modules as subcomponents. Image
 * scanners (Trivy) see the main module as the parent of its dependencies in a Go
 * binary's build information, so a statement applies to the vulnerable module
 * inside OCM's binaries only, never to the same module in another binary of the
 * image (cosign), which govulncheck did not analyze. Statements carry the
 * advisory's aliases (CVE IDs), the IDs image scanners report.
 *
 * Other binaries in the images get the statements their publisher attested:
 * importAttested() takes a verified OpenVEX attestation (gh attestation verify
 * output) and scopes its statements to that binary's main module the same way.
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
    const main = messages.find((m) => m.SBOM)?.SBOM?.modules?.find((/** @type {any} */ m) => !m.version)?.path;
    if (!main) throw new Error("govulncheck output names no main module (SBOM message without an unversioned module)");
    /** @type {Map<string, string[]>} */
    const aliases = new Map(messages.filter((m) => m.osv).map((m) => [m.osv.id, m.osv.aliases ?? []]));
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
        vulnerability: { "@id": `https://pkg.go.dev/vuln/${osv}`, name: osv, aliases: [...(aliases.get(osv) ?? [])].sort() },
        products: [{ "@id": `pkg:golang/${main}`, subcomponents: [...e.products].sort().map((id) => ({ "@id": id })) }],
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
 * govulncheck writes Go package URLs with the module path percent-encoded
 * (pkg:golang/golang.org%2Fx%2Fcrypto@v0.58.0); scanners use the plain path
 * (pkg:golang/golang.org/x/crypto@v0.58.0), and the encoded form does not match it.
 *
 * @param {string} purl
 */
export function normalizeGoPurl(purl) {
    const m = purl.match(/^pkg:golang\/([^@?#]+)(.*)$/);
    return m ? `pkg:golang/${decodeURIComponent(m[1])}${m[2]}` : purl;
}

/**
 * Digest of an OCI package URL (pkg:oci/name@sha256%3A...), or undefined.
 *
 * @param {string} purl
 */
function ociDigest(purl) {
    const m = purl.match(/^pkg:oci\/[^@?#]+@([^?#]+)/);
    return m ? decodeURIComponent(m[1]) : undefined;
}

/**
 * Statements of verified OpenVEX attestations about the image `target.subject`
 * (<repository>@sha256:<digest>), scoped to the main module of the binary the
 * image provides. Only products naming that image digest (pkg:oci/...@<digest>)
 * count, so an attestation that does not say which build it is about cannot
 * suppress anything. A statement without such a product, or whose product names
 * no subcomponent (no module; scoping it would cover every module of the
 * binary), is skipped.
 *
 * @param {any[]} verified - `gh attestation verify --format json` output
 * @param {{ product: string, subject: string }} target - main module purl of the binary, and the attested image by digest
 * @param {(id: string) => Promise<string[]>} aliasesOf - aliases of an advisory
 * @returns {Promise<{ statements: any[], skipped: string[] }>}
 */
export async function importAttested(verified, target, aliasesOf) {
    const digest = target.subject.split("@")[1];
    if (!digest?.startsWith("sha256:")) throw new Error(`attested image ${target.subject} is not referenced by digest`);
    const statements = [];
    const skipped = [];
    for (const v of verified) {
        const signer = v.verificationResult?.signature?.certificate?.buildSignerURI ?? "unknown signer";
        for (const s of v.verificationResult?.statement?.predicate?.statements ?? []) {
            const name = s.vulnerability?.name;
            const products = (s.products ?? []).filter((/** @type {any} */ p) => ociDigest(p["@id"] ?? "") === digest);
            const subcomponents = [...new Set(products.flatMap((/** @type {any} */ p) => (p.subcomponents ?? []).map((/** @type {any} */ c) => normalizeGoPurl(c["@id"]))))].sort();
            if (!name || subcomponents.length === 0) {
                skipped.push(name ?? "statement without vulnerability");
                continue;
            }
            const aliases = [...new Set([...(s.vulnerability.aliases ?? []), ...(await aliasesOf(name))])].sort();
            statements.push({
                vulnerability: { ...(name.startsWith("GO-") && { "@id": `https://pkg.go.dev/vuln/${name}` }), name, aliases },
                products: [{ "@id": target.product, subcomponents: subcomponents.map((id) => ({ "@id": id })) }],
                status: s.status,
                ...(s.justification && { justification: s.justification }),
                ...(s.impact_statement && { impact_statement: s.impact_statement }),
                ...(s.action_statement && { action_statement: s.action_statement }),
                status_notes: `From the OpenVEX attestation of ${target.subject}, signed by ${signer}.`,
            });
        }
    }
    return { statements, skipped };
}

/**
 * Aliases of a Go advisory from the Go vulnerability database.
 *
 * @param {string} id
 * @returns {Promise<string[]>}
 */
async function goAliases(id) {
    if (!id.startsWith("GO-")) return [];
    const res = await fetch(`https://vuln.go.dev/ID/${id}.json`);
    if (!res.ok) throw new Error(`vuln.go.dev: ${id}: HTTP ${res.status}`);
    return (await res.json()).aliases ?? [];
}

/**
 * GitHub Actions entrypoint.
 *
 * Environment variables:
 * - GOVULNCHECK_JSON: govulncheck -format json output (required)
 * - VEX_OUTPUT: path of the OpenVEX document to write (required)
 * - VEX_PLATFORM: os/arch govulncheck analyzed, e.g. linux/amd64 (required)
 * - ATTESTED_VEX: `gh attestation verify --format json` output of another
 *   binary's OpenVEX attestation, merged into the document (optional)
 * - ATTESTED_VEX_PRODUCT: main module purl of that binary (required with ATTESTED_VEX)
 * - ATTESTED_VEX_SUBJECT: the attested image as <repository>@sha256:<digest> (required with ATTESTED_VEX)
 *
 * @param {import('@actions/github-script').AsyncFunctionArguments} args
 */
export default async function vexAction({ core, context }) {
    const { GOVULNCHECK_JSON: input, VEX_OUTPUT: output, VEX_PLATFORM: platform } = process.env;
    if (!input || !output || !platform) {
        core.setFailed("GOVULNCHECK_JSON, VEX_OUTPUT and VEX_PLATFORM environment variables are required");
        return;
    }
    const vex = buildVex(parseJsonStream(await readFile(input, "utf8")), {
        id: `${context.serverUrl}/${context.repo.owner}/${context.repo.repo}/actions/runs/${context.runId}/vex/${platform}`,
        author: `${context.repo.owner}/${context.repo.repo}`,
        timestamp: new Date().toISOString(),
    });
    const { ATTESTED_VEX: attested, ATTESTED_VEX_PRODUCT: product, ATTESTED_VEX_SUBJECT: subject } = process.env;
    if (attested) {
        if (!product || !subject) {
            core.setFailed("ATTESTED_VEX_PRODUCT and ATTESTED_VEX_SUBJECT are required with ATTESTED_VEX");
            return;
        }
        const imported = await importAttested(JSON.parse(await readFile(attested, "utf8")), { product, subject }, goAliases);
        for (const name of imported.skipped) core.warning(`${subject}: skipped ${name}: no product names this image digest with a module`);
        core.info(`${subject}: imported ${imported.statements.length} attested statement(s) for ${product}`);
        vex.statements.push(...imported.statements);
    }
    await writeFile(output, JSON.stringify(vex, null, 2));
    const affected = vex.statements.filter((s) => s.status === "affected");
    core.info(`OpenVEX (${platform}): ${vex.statements.length} statement(s), ${affected.length} affected`);
    await core.summary
        .addHeading(`📄 OpenVEX from govulncheck (${platform})`, 3)
        .addTable([
            [{ data: "Advisory", header: true }, { data: "Status", header: true }, { data: "Binary (main module)", header: true }, { data: "Modules", header: true }],
            ...vex.statements.map((s) => [
                [s.vulnerability.name, ...s.vulnerability.aliases].join(", "),
                s.status + (s.justification ? ` (${s.justification})` : ""),
                s.products[0]["@id"],
                s.products.flatMap((/** @type {any} */ p) => p.subcomponents.map((/** @type {any} */ c) => c["@id"])).join(", "),
            ]),
        ])
        .write();
}
