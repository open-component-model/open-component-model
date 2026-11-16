import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { checkImage, REQUIRED_ANNOTATIONS } from "./image-metadata.js";

const ann = (keys) => Object.fromEntries(keys.map((k) => [`org.opencontainers.image.${k}`, "x"]));
const amd64 = { os: "linux", architecture: "amd64" };

/** OCI archive layout as docker buildx writes it: index.json -> image index -> platform and attestation manifests. */
function archive({ annotations = REQUIRED_ANNOTATIONS, provenanceFor = "sha256:amd", predicate = "https://slsa.dev/provenance/v1" } = {}) {
    const blobs = {
        "sha256:idx": {
            manifests: [
                { digest: "sha256:amd", platform: amd64 },
                { digest: "sha256:arm", platform: { os: "linux", architecture: "arm64" } },
                { digest: "sha256:att", platform: { os: "unknown", architecture: "unknown" }, annotations: { "vnd.docker.reference.type": "attestation-manifest", "vnd.docker.reference.digest": provenanceFor } },
            ],
        },
        "sha256:amd": { annotations: ann(annotations) },
        "sha256:arm": { annotations: ann(REQUIRED_ANNOTATIONS) },
        "sha256:att": { layers: [{ digest: "sha256:l", annotations: { "in-toto.io/predicate-type": predicate } }] },
    };
    return { index: { manifests: [{ digest: "sha256:idx" }] }, blob: async (d) => blobs[d] };
}

const results = async (a, platform = amd64) => Object.fromEntries((await checkImage(a.blob, a.index, platform)).map((c) => [c.id, c.result]));

describe("checkImage", () => {
    it("passes an image with all annotations and a provenance attestation, through nested indexes", async () => {
        assert.deepEqual(await results(archive()), { annotations: "pass", provenance: "pass" });
    });

    it("names the missing annotations", async () => {
        const a = archive({ annotations: REQUIRED_ANNOTATIONS.filter((k) => k !== "revision" && k !== "source") });
        const [annotations] = await checkImage(a.blob, a.index, amd64);
        assert.equal(annotations.result, "fail");
        assert.match(annotations.detail, /revision,source/);
    });

    it("does not count provenance attested for another platform", async () => {
        assert.equal((await results(archive({ provenanceFor: "sha256:arm" }))).provenance, "fail");
    });

    it("does not count attestations that are not SLSA provenance", async () => {
        assert.equal((await results(archive({ predicate: "https://spdx.dev/Document" }))).provenance, "fail");
    });

    it("fails both checks when the platform is not in the archive", async () => {
        assert.deepEqual(await results(archive(), { os: "linux", architecture: "s390x" }), { annotations: "fail", provenance: "fail" });
    });
});
