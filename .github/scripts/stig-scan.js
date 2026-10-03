// @ts-check
import { execFile } from "node:child_process";
import { createHash } from "node:crypto";
import { cp, mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { promisify } from "node:util";

/**
 * Scans an OCM container image against the DISA GPOS SRG, as tailored for
 * scratch images in .github/stig, and fails on any failed or errored rule.
 *
 * The scan runs offline on the exported root filesystem of the image, inside the
 * OpenSCAP image pinned as OPENSCAP_IMAGE in the root .env: no privileged
 * container and no docker socket. The digest-pinned base image of the image's
 * "certs" build stage is the expected source of its CA bundle.
 *
 * Locally: node .github/scripts/stig-scan.js <image> <containerfile> <output-dir>
 */

const repo = path.resolve(import.meta.dirname, "../..");
const run = promisify(execFile);
const FAILING = new Set(["fail", "error", "unknown"]);

/**
 * @typedef {object} RuleResult
 * @property {string} id - XCCDF rule id
 * @property {string} title - rule title
 * @property {string} result - XCCDF result (pass, fail, notapplicable, ...)
 * @property {string[]} references - referenced SRG rules (SV-...)
 */

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
 * Read Go build settings (the "build" lines `go version -m` shows) from a Go
 * binary. OVAL file probes stop at the first NUL byte and cannot read them.
 *
 * @param {Buffer} binary - contents of a Go executable
 * @returns {Record<string, string>}
 */
export function goBuildSettings(binary) {
    /** @type {Record<string, string>} */
    const settings = {};
    for (const m of binary.toString("latin1").matchAll(/(?:^|\n)build\t([A-Za-z0-9_.-]+)=([^\n]*)/g)) {
        settings[m[1]] ??= m[2];
    }
    return settings;
}

/**
 * Names of the OVAL tests that did not pass, by their comment.
 *
 * @param {string} ovalResults - OVAL results document written by oscap --oval-results
 * @param {string} ovalDefinitions - the OVAL definitions document
 * @returns {string[]}
 */
export function failedOvalTests(ovalResults, ovalDefinitions) {
    const ids = new Set([...ovalResults.matchAll(/<test test_id="([^"]+)"[^>]*result="(?:false|error|unknown)"/g)].map((m) => m[1]));
    const comments = new Map([...ovalDefinitions.matchAll(/_test id="([^"]+)"[^>]*comment="([^"]+)"/g)].map((m) => [m[1], m[2]]));
    return [...ids].map((id) => comments.get(id) ?? id);
}

/** @param {string} s */
const xml = (s) => s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");

/** @param {string[]} args */
async function docker(...args) {
    const { stdout } = await run("docker", args, { maxBuffer: 64 * 1024 * 1024 });
    return stdout.trim();
}

// Runs inside the OpenSCAP image. Files docker create/export add to every
// container are removed first; they are not image content.
const SCAN_SH = `set -u
ds=/usr/share/xml/scap/ssg/content/ssg-chainguard-gpos-ds.xml
mkdir -p /work/out /work/rootfs
tar -C /work/rootfs -xpf /work/rootfs.tar || { echo "extract $?" >>/work/out/exit; exit 1; }
rm -rf /work/rootfs/.dockerenv /work/rootfs/etc/hosts /work/rootfs/etc/hostname /work/rootfs/etc/resolv.conf \\
	/work/rootfs/etc/mtab /work/rootfs/dev /work/rootfs/proc /work/rootfs/sys
cd /work/out
export OSCAP_PROBE_ROOT=/work/rootfs
oscap xccdf eval --tailoring-file /work/stig/tailoring.xml --profile xccdf_software.ocm_profile_gpos-container \\
	--results gpos-results.xml --report gpos-report.html "$ds" >gpos.log 2>&1
echo "gpos $?" >>exit
oscap xccdf eval --tailoring-file /work/stig/values.xml --profile xccdf_software.ocm_profile_supplement-values \\
	--oval-results --results ocm-results.xml --report ocm-report.html /work/stig/ocm-supplement-xccdf.xml >ocm.log 2>&1
echo "ocm $?" >>exit
`;

/**
 * Run the scan; results land in `out`.
 *
 * @param {string} image - image reference in the local docker daemon
 * @param {string} containerfile - the image's build file
 * @param {string} out - output directory
 */
export async function scan(image, containerfile, out) {
    const openscap = (await readFile(path.join(repo, ".env"), "utf8")).match(/^OPENSCAP_IMAGE=(\S+)$/m)?.[1];
    if (!openscap) throw new Error(`OPENSCAP_IMAGE not set in ${repo}/.env`);
    const base = (await readFile(containerfile, "utf8")).match(/^FROM .*\s(ghcr\.io\/gardenlinux\/\S+@sha256:[a-f0-9]{64})\s+AS\s+certs$/m)?.[1];
    if (!base) throw new Error(`no digest-pinned Garden Linux certs stage in ${containerfile}`);
    const entrypoint = await docker("image", "inspect", "-f", "{{index .Config.Entrypoint 0}}", image);
    if (!entrypoint.startsWith("/")) throw new Error(`image ${image} has no absolute entrypoint`);

    const work = await mkdtemp(path.join(tmpdir(), "stig-"));
    /** @type {string[]} */
    const containers = [];
    try {
        // Expected CA bundle digest, taken from the pinned base image.
        const arch = await docker("version", "-f", "{{.Server.Arch}}");
        containers.push(await docker("create", "--platform", `linux/${arch}`, base, "/bin/true"));
        await docker("cp", "-q", `${containers.at(-1)}:/etc/ssl/certs/ca-certificates.crt`, path.join(work, "ca.crt"));
        const caSha256 = createHash("sha256").update(await readFile(path.join(work, "ca.crt"))).digest("hex");

        // Root filesystem of the image under test. It is extracted inside the
        // scanner as root: extracting it here as a regular user would drop
        // setuid/setgid bits and ownership, which the checks inspect.
        containers.push(await docker("create", image, "/x"));
        const rootfs = path.join(work, "rootfs.tar");
        await docker("export", "-o", rootfs, containers.at(-1));
        const { stdout: binary } = await run("tar", ["-xOf", rootfs, entrypoint.slice(1)], { encoding: "buffer", maxBuffer: 1024 * 1024 * 1024 });
        const settings = goBuildSettings(binary);

        await cp(path.join(repo, ".github/stig"), path.join(work, "stig"), { recursive: true });
        await writeFile(path.join(work, "stig/values.xml"), `<?xml version="1.0" encoding="UTF-8"?>
<Tailoring xmlns="http://checklists.nist.gov/xccdf/1.2" id="xccdf_software.ocm_tailoring_supplement-values">
  <benchmark href="/work/stig/ocm-supplement-xccdf.xml"/>
  <version time="${new Date().toISOString().slice(0, 19)}">1</version>
  <Profile id="xccdf_software.ocm_profile_supplement-values" extends="xccdf_software.ocm_profile_supplement">
    <title>OCM GPOS SRG supplement for ${xml(image)}</title>
    <set-value idref="xccdf_software.ocm_value_entrypoint">${xml(entrypoint)}</set-value>
    <set-value idref="xccdf_software.ocm_value_expected_ca_sha256">${caSha256}</set-value>
    <set-value idref="xccdf_software.ocm_value_gofips140">${xml(settings.GOFIPS140 ?? "")}</set-value>
    <set-value idref="xccdf_software.ocm_value_default_godebug">${xml(settings.DefaultGODEBUG ?? "")}</set-value>
  </Profile>
</Tailoring>
`);
        await writeFile(path.join(work, "scan.sh"), SCAN_SH);

        containers.push(await docker("create", "-u", "0:0", "--entrypoint", "/bin/sh", openscap, "/work/scan.sh"));
        await docker("cp", "-q", `${work}/.`, `${containers.at(-1)}:/work`);
        await docker("start", "-a", containers.at(-1));
        await mkdir(out, { recursive: true });
        await docker("cp", "-q", `${containers.at(-1)}:/work/out/.`, out);
    } finally {
        if (containers.length > 0) await run("docker", ["rm", "-f", ...containers]).catch(() => {});
        await rm(work, { recursive: true, force: true });
    }
}

/**
 * Evaluate the scan output.
 *
 * @param {string} out - output directory of scan()
 * @returns {Promise<{evaluations: Record<string, RuleResult[]>, errors: string[], failedTests: string[]}>}
 */
export async function evaluate(out) {
    /** @type {string[]} */
    const errors = [];
    const exits = await readFile(path.join(out, "exit"), "utf8").catch(() => "");
    for (const [, name, code] of exits.matchAll(/^(\S+) (\d+)$/gm)) {
        // oscap exits 2 when a rule fails; anything else non-zero is an evaluation error.
        if (code !== "0" && code !== "2") errors.push(`${name} evaluation error (exit ${code}), see ${out}/${name}.log`);
    }

    /** @type {Record<string, RuleResult[]>} */
    const evaluations = {};
    for (const [name, file] of [["GPOS SRG (tailored Chainguard profile)", "gpos-results.xml"], ["OCM supplement", "ocm-results.xml"]]) {
        const content = await readFile(path.join(out, file), "utf8").catch(() => undefined);
        if (content === undefined) errors.push(`missing ${file}`);
        else evaluations[name] = parseXccdfResults(content);
    }

    const ovalResults = await readFile(path.join(out, "ocm-supplement-oval.xml.result.xml"), "utf8").catch(() => "");
    const failedTests = failedOvalTests(ovalResults, await readFile(path.join(repo, ".github/stig/ocm-supplement-oval.xml"), "utf8"));
    return { evaluations, errors, failedTests };
}

/**
 * GitHub Actions entrypoint: scans the image, logs and summarizes the results,
 * and fails on any failed rule or evaluation error.
 *
 * Environment variables:
 * - IMAGE_REF: image reference in the local docker daemon (required)
 * - CONTAINERFILE: the image's build file (required)
 * - RESULTS_DIR: output directory for results and reports (required)
 * - SUMMARY_TITLE: heading of the job summary; no summary is written without it
 *
 * @param {import('@actions/github-script').AsyncFunctionArguments} args
 */
export default async function stigScanAction({ core }) {
    const { IMAGE_REF: image, CONTAINERFILE: containerfile, RESULTS_DIR: out, SUMMARY_TITLE: title } = process.env;
    if (!image || !containerfile || !out) {
        core.setFailed("IMAGE_REF, CONTAINERFILE and RESULTS_DIR environment variables are required");
        return;
    }

    /** @type {string[]} */
    const errors = [];
    try {
        await scan(image, containerfile, out);
    } catch (error) {
        errors.push(`scan failed: ${error instanceof Error ? error.message : error}`);
    }
    const result = await evaluate(out);
    errors.push(...result.errors);

    const failed = Object.values(result.evaluations).flat().filter((r) => FAILING.has(r.result));
    for (const [name, results] of Object.entries(result.evaluations)) {
        core.info(`${name}: ${Object.entries(countResults(results)).map(([k, v]) => `${v} ${k}`).join(", ")}`);
    }
    for (const r of failed) core.error(`${r.title || r.id} (${r.id}): ${r.result}`);
    for (const t of result.failedTests) core.error(`OVAL test failed: ${t}`);

    if (title && process.env.GITHUB_STEP_SUMMARY) {
        const columns = ["pass", "fail", "error", "notapplicable", "notselected"];
        const summary = core.summary.addHeading(`🛡️ STIG Scan: ${title}`).addTable([
            [{ data: "Evaluation", header: true }, ...columns.map((c) => ({ data: c, header: true }))],
            ...Object.entries(result.evaluations).map(([name, results]) => {
                const counts = countResults(results);
                return [name, ...columns.map((c) => String(counts[c] ?? 0))];
            }),
        ]);
        const supplement = result.evaluations["OCM supplement"] ?? [];
        if (supplement.length > 0) {
            summary.addHeading("OCM checks", 3).addTable([
                [{ data: "Check", header: true }, { data: "SRG rules", header: true }, { data: "Result", header: true }],
                ...supplement.map((r) => [r.title, r.references.join(", "), FAILING.has(r.result) ? `❌ ${r.result}` : `✅ ${r.result}`]),
            ]);
        }
        if (failed.length > 0) {
            summary.addHeading("Failed rules", 3).addList(failed.map((r) => `${r.title || r.id} (${r.id}): ${r.result}`));
        }
        if (result.failedTests.length > 0) summary.addHeading("Failed OVAL tests", 3).addList(result.failedTests);
        if (errors.length > 0) summary.addHeading("Errors", 3).addList(errors);
        await summary.write();
    }

    if (errors.length > 0 || failed.length > 0) {
        core.setFailed([...errors, ...(failed.length > 0 ? [`${failed.length} STIG rule(s) failed`] : [])].join("; "));
    }
}

if (import.meta.main) {
    const [image, containerfile, out] = process.argv.slice(2);
    if (!image || !containerfile || !out) {
        console.error("usage: node .github/scripts/stig-scan.js <image> <containerfile> <output-dir>");
        process.exit(2);
    }
    Object.assign(process.env, { IMAGE_REF: image, CONTAINERFILE: containerfile, RESULTS_DIR: out });
    /** @type {any} */
    const core = {
        info: console.log,
        error: (/** @type {string} */ m) => console.error(`FAILED: ${m}`),
        setFailed: (/** @type {string} */ m) => {
            console.error(m);
            process.exitCode = 1;
        },
    };
    await stigScanAction({ core });
}
