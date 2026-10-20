// @ts-check
import { execFile } from "node:child_process";
import { writeFile } from "node:fs/promises";
import { promisify } from "node:util";

/**
 * Checks that an image in an OCI archive carries the metadata that makes its
 * function and history traceable (BSI IT-Grundschutz SYS.1.6.A12): the
 * pre-defined OCI annotations on the platform manifest (set by
 * oci-annotations.sh) and a SLSA provenance attestation for that manifest
 * (BuildKit attestation manifest).
 */

const run = promisify(execFile);

/** Annotations every image manifest must carry, without the org.opencontainers.image. prefix. */
export const REQUIRED_ANNOTATIONS = ["title", "description", "version", "revision", "source", "created", "vendor", "licenses"];
const PROVENANCE = /^https:\/\/slsa\.dev\/provenance\/v\d/;

/**
 * @typedef {{ mediaType?: string, digest: string, platform?: { os: string, architecture: string }, annotations?: Record<string, string> }} Descriptor
 * @typedef {{ manifests?: Descriptor[], layers?: Descriptor[], annotations?: Record<string, string> }} Manifest
 * @typedef {{ id: string, result: "pass" | "fail", detail: string }} Check
 */

/**
 * Evaluate the image of one platform.
 *
 * @param {(digest: string) => Promise<Manifest>} blob - reads a manifest or index blob by digest
 * @param {Manifest} index - the archive's index.json
 * @param {{ os: string, architecture: string }} platform
 * @returns {Promise<Check[]>}
 */
export async function checkImage(blob, index, platform) {
    // Indexes may nest (docker save wraps the image index in one more index).
    /** @type {Descriptor | undefined} */
    let image;
    /** @type {Descriptor[]} */
    const attestations = [];
    /** @param {Manifest} node */
    const walk = async (node) => {
        for (const d of node.manifests ?? []) {
            if (d.annotations?.["vnd.docker.reference.type"] === "attestation-manifest") attestations.push(d);
            else if (d.platform?.os === platform.os && d.platform?.architecture === platform.architecture) image ??= d;
            else if (!d.platform) await walk(await blob(d.digest));
        }
    };
    await walk(index);
    const name = `${platform.os}/${platform.architecture}`;
    if (!image) {
        return [
            { id: "annotations", result: "fail", detail: `no ${name} manifest` },
            { id: "provenance", result: "fail", detail: `no ${name} manifest` },
        ];
    }

    const annotations = (await blob(image.digest)).annotations ?? {};
    const missing = REQUIRED_ANNOTATIONS.filter((a) => !annotations[`org.opencontainers.image.${a}`]);
    /** @type {Check[]} */
    const checks = [{
        id: "annotations",
        result: missing.length === 0 ? "pass" : "fail",
        detail: missing.length === 0 ? `${REQUIRED_ANNOTATIONS.length} required annotations present` : `missing org.opencontainers.image.{${missing.join(",")}}`,
    }];

    const forImage = attestations.filter((a) => a.annotations?.["vnd.docker.reference.digest"] === image?.digest);
    const predicates = [];
    for (const a of forImage) {
        for (const layer of (await blob(a.digest)).layers ?? []) {
            const type = layer.annotations?.["in-toto.io/predicate-type"];
            if (type) predicates.push(type);
        }
    }
    const provenance = predicates.find((p) => PROVENANCE.test(p));
    checks.push({
        id: "provenance",
        result: provenance ? "pass" : "fail",
        detail: provenance ? `attestation ${provenance}` : `no SLSA provenance attestation for the ${name} manifest`,
    });
    return checks;
}

/**
 * Read blobs from an OCI archive (tar) with the system tar.
 *
 * @param {string} archive
 * @param {string} file
 */
async function readMember(archive, file) {
    const { stdout } = await run("tar", ["-xOf", archive, file], { maxBuffer: 64 * 1024 * 1024 });
    return JSON.parse(stdout);
}

/**
 * GitHub Actions entrypoint.
 *
 * Environment variables:
 * - OCI_ARCHIVE: OCI image archive (required)
 * - ARCH: architecture to check (required); the OS is linux
 * - OUTPUT: path of the JSON result (required)
 * - SUMMARY_TITLE: heading of the job summary; no summary is written without it
 *
 * @param {import('@actions/github-script').AsyncFunctionArguments} args
 */
export default async function imageMetadataAction({ core }) {
    const { OCI_ARCHIVE: archive, ARCH: architecture, OUTPUT: output, SUMMARY_TITLE: title } = process.env;
    if (!archive || !architecture || !output) {
        core.setFailed("OCI_ARCHIVE, ARCH and OUTPUT environment variables are required");
        return;
    }
    const blob = (/** @type {string} */ digest) => readMember(archive, `blobs/${digest.replace(":", "/")}`);
    const checks = await checkImage(blob, await readMember(archive, "index.json"), { os: "linux", architecture });
    await writeFile(output, `${JSON.stringify(checks, null, 2)}\n`);

    for (const c of checks) (c.result === "pass" ? core.info : core.error)(`${c.id}: ${c.detail}`);
    if (title && process.env.GITHUB_STEP_SUMMARY) {
        await core.summary.addHeading(`🏷️ Image metadata: ${title}`, 3)
            .addTable([
                ["Check", "Result", "Detail"].map((data) => ({ data, header: true })),
                ...checks.map((c) => [c.id, c.result === "pass" ? "✅ pass" : "❌ fail", c.detail]),
            ])
            .write();
    }
    const failed = checks.filter((c) => c.result === "fail");
    if (failed.length > 0) core.setFailed(`image metadata: ${failed.map((c) => c.id).join(", ")} failed`);
}
