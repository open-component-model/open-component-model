// @ts-check
import { readFile, writeFile } from "node:fs/promises";
import path from "node:path";

/**
 * Checks that an image in an OCI image layout carries the metadata that makes its
 * function and history traceable (BSI IT-Grundschutz SYS.1.6.A12): the
 * pre-defined OCI annotations on the platform manifest (set by
 * oci-annotations.sh) and a SLSA provenance attestation whose in-toto statement
 * names that manifest (BuildKit attestation manifest).
 */

const DIGEST = /^[a-z0-9]+(?:[.+_-][a-z0-9]+)*:[a-zA-Z0-9=_-]+$/;

/** Annotations every image manifest must carry, without the org.opencontainers.image. prefix. */
export const REQUIRED_ANNOTATIONS = ["title", "description", "version", "revision", "source", "created", "vendor", "licenses"];
const PROVENANCE = /^https:\/\/slsa\.dev\/provenance\/v\d/;
const STATEMENT = /^https:\/\/in-toto\.io\/Statement\/v\d/;

/**
 * @typedef {{ mediaType?: string, digest: string, platform?: { os: string, architecture: string }, annotations?: Record<string, string> }} Descriptor
 * @typedef {{ name?: string, digest?: Record<string, string> }} Subject
 * @typedef {{ manifests?: Descriptor[], layers?: Descriptor[], annotations?: Record<string, string>, _type?: string, predicateType?: string, predicate?: unknown, subject?: Subject[] }} Blob
 * @typedef {{ id: string, result: "pass" | "fail", detail: string }} Check
 */

/**
 * Validate the in-toto statement an attestation layer holds.
 *
 * The layer annotation only advertises the predicate type; the statement in the
 * blob carries the subject, which is what ties the provenance to this image
 * (BuildKit names the platform manifest there).
 *
 * @param {(digest: string) => Promise<Blob>} blob
 * @param {string} layer - digest of the in-toto layer
 * @param {string} manifest - digest of the platform manifest the provenance must be about
 * @returns {Promise<string | { predicateType: string }>} the reason it is not provenance for the manifest, or the predicate type
 */
async function provenanceStatement(blob, layer, manifest) {
    /** @type {Blob | undefined} */
    let statement;
    try {
        statement = await blob(layer);
    } catch (err) {
        return `${layer}: unreadable (${err instanceof Error ? err.message : err})`;
    }
    if (!statement || typeof statement !== "object") return `${layer}: no in-toto statement`;
    if (!STATEMENT.test(statement._type ?? "")) return `${layer}: _type ${statement._type ?? "(none)"} is not an in-toto statement`;
    if (!PROVENANCE.test(statement.predicateType ?? "")) return `${layer}: predicateType ${statement.predicateType ?? "(none)"} is not SLSA provenance`;
    if (!statement.predicate || typeof statement.predicate !== "object") return `${layer}: statement carries no predicate`;
    const subjects = (statement.subject ?? []).flatMap((s) => Object.entries(s.digest ?? {}).map(([algorithm, hex]) => `${algorithm}:${hex}`));
    if (subjects.length === 0) return `${layer}: statement names no subject digest`;
    if (!subjects.includes(manifest)) return `${layer}: statement is about ${[...new Set(subjects)].join(", ")}`;
    return { predicateType: statement.predicateType ?? "" };
}

/**
 * Evaluate the image of one platform.
 *
 * @param {(digest: string) => Promise<Blob>} blob - reads a manifest, index or statement blob by digest
 * @param {Blob} index - the archive's index.json
 * @param {{ os: string, architecture: string }} platform
 * @returns {Promise<Check[]>}
 */
export async function checkImage(blob, index, platform) {
    // Indexes may nest (docker save wraps the image index in one more index).
    /** @type {Descriptor | undefined} */
    let image;
    /** @type {Descriptor[]} */
    const attestations = [];
    /** @param {Blob} node @param {number} depth - index nesting left to follow */
    const walk = async (node, depth) => {
        for (const d of node.manifests ?? []) {
            if (d.annotations?.["vnd.docker.reference.type"] === "attestation-manifest") attestations.push(d);
            else if (d.platform?.os === platform.os && d.platform?.architecture === platform.architecture) image ??= d;
            else if (!d.platform && depth > 0) await walk(await blob(d.digest), depth - 1);
        }
    };
    await walk(index, 2);
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
    /** @type {string[]} */
    const rejected = [];
    /** @type {string | undefined} */
    let provenance;
    for (const a of forImage) {
        for (const layer of (await blob(a.digest)).layers ?? []) {
            if (!PROVENANCE.test(layer.annotations?.["in-toto.io/predicate-type"] ?? "")) continue;
            const statement = await provenanceStatement(blob, layer.digest, image.digest);
            if (typeof statement === "string") rejected.push(statement);
            else provenance ??= statement.predicateType;
        }
    }
    checks.push({
        id: "provenance",
        result: provenance ? "pass" : "fail",
        detail: provenance ? `attestation ${provenance} for the ${name} manifest`
            : rejected.length > 0 ? `no valid SLSA provenance for the ${name} manifest: ${rejected.join("; ")}`
                : `no SLSA provenance attestation for the ${name} manifest`,
    });
    return checks;
}

/**
 * Read a JSON blob of an extracted OCI image layout.
 *
 * @param {string} layout - directory of the layout
 * @param {string} digest
 * @returns {Promise<Blob>}
 */
async function readBlob(layout, digest) {
    if (!DIGEST.test(digest)) throw new Error(`malformed digest ${digest}`);
    const [algorithm, hex] = digest.split(":");
    return JSON.parse(await readFile(path.join(layout, "blobs", algorithm, hex), "utf8"));
}

/**
 * GitHub Actions entrypoint.
 *
 * Environment variables:
 * - OCI_LAYOUT: directory of the extracted OCI image layout (required)
 * - OCI_INDEX: index to start from (default <OCI_LAYOUT>/index.json); the scan
 *   job narrows the layout's own index to one platform, which drops the
 *   attestation manifests this check needs, and keeps the original here.
 * - ARCH: architecture to check (required); the OS is linux
 * - OUTPUT: path of the JSON result (required)
 * - SUMMARY_TITLE: heading of the job summary; no summary is written without it
 *
 * @param {import('@actions/github-script').AsyncFunctionArguments} args
 */
export default async function imageMetadataAction({ core }) {
    const { OCI_LAYOUT: layout, OCI_INDEX: indexFile, ARCH: architecture, OUTPUT: output, SUMMARY_TITLE: title } = process.env;
    if (!layout || !architecture || !output) {
        core.setFailed("OCI_LAYOUT, ARCH and OUTPUT environment variables are required");
        return;
    }
    const index = JSON.parse(await readFile(indexFile || path.join(layout, "index.json"), "utf8"));
    const checks = await checkImage((digest) => readBlob(layout, digest), index, { os: "linux", architecture });
    await writeFile(output, `${JSON.stringify(checks, null, 2)}\n`);

    for (const c of checks) {
        const line = `${c.id}: ${c.detail}`;
        if (c.result === "pass") core.info(line);
        else core.error(line);
    }
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
