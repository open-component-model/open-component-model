import assert from "assert";
import fs from "fs";
import os from "os";
import path from "path";
import yaml from "js-yaml";
import {
    extractNameVersion,
    buildPackageUrl,
    patchCliConstructor,
    parseConstructorFile,
} from "./component-constructor.js";

// ----------------------------------------------------------
// extractNameVersion
// ----------------------------------------------------------
console.log("Testing extractNameVersion...");

assert.deepStrictEqual(
    extractNameVersion({ name: "ocm.software/cli", version: "1.2.3" }),
    { name: "ocm.software/cli", version: "1.2.3" },
    "Should extract name and version from valid constructor"
);

assert.deepStrictEqual(
    extractNameVersion({ name: "ocm.software/plugins/helm", version: "0.0.0-main" }),
    { name: "ocm.software/plugins/helm", version: "0.0.0-main" },
    "Should handle nested component names"
);

assert.throws(
    () => extractNameVersion({ version: "1.0.0" }),
    /missing required field 'name'/,
    "Should throw when name is missing"
);

assert.throws(
    () => extractNameVersion({ name: "foo" }),
    /missing required field 'version'/,
    "Should throw when version is missing"
);

assert.throws(
    () => extractNameVersion({ name: "", version: "1.0.0" }),
    /missing required field 'name'/,
    "Should throw when name is empty"
);

assert.throws(
    () => extractNameVersion({ name: 42, version: "1.0.0" }),
    /missing required field 'name'/,
    "Should throw when name is not a string"
);

// ----------------------------------------------------------
// buildPackageUrl
// ----------------------------------------------------------
console.log("Testing buildPackageUrl...");

assert.strictEqual(
    buildPackageUrl("open-component-model/open-component-model", "ocm.software/cli"),
    "https://github.com/open-component-model/open-component-model/pkgs/container/component-descriptors%2Focm.software%2Fcli",
    "Should build correct package URL with encoded slashes"
);

assert.strictEqual(
    buildPackageUrl("my-org/my-repo", "simple"),
    "https://github.com/my-org/my-repo/pkgs/container/component-descriptors%2Fsimple",
    "Should handle component name without slashes"
);

assert.strictEqual(
    buildPackageUrl("org/repo", "a/b/c/d"),
    "https://github.com/org/repo/pkgs/container/component-descriptors%2Fa%2Fb%2Fc%2Fd",
    "Should encode all slashes in deeply nested component names"
);

// ----------------------------------------------------------
// patchCliConstructor
// ----------------------------------------------------------
console.log("Testing patchCliConstructor...");

{
    const constructor = {
        name: "ocm.software/cli",
        version: "1.0.0",
        resources: [
            {
                name: "cli",
                type: "executable",
                input: { type: "file", path: "/full/absolute/path/to/bin/ocm-linux-amd64" },
                extraIdentity: { os: "linux", architecture: "amd64" },
                relation: "local",
            },
            {
                name: "cli",
                type: "executable",
                input: { type: "file", path: "/another/path/bin/ocm-darwin-arm64" },
                extraIdentity: { os: "darwin", architecture: "arm64" },
                relation: "local",
            },
            {
                name: "image",
                type: "ociImage",
                version: "old",
                relation: "local",
                input: { type: "file", mediaType: "application/vnd.ocm.software.oci.layout.v1+tar", path: "/path/to/cli.tar" },
            },
        ],
    };

    const result = patchCliConstructor(constructor, "ghcr.io/ocm/cli:v1.0.0", "v1.0.0");

    // CLI binary paths should be rewritten
    assert.strictEqual(
        result.resources[0].input.path,
        "resources/bin/ocm-linux-amd64",
        "Should rewrite first CLI binary path"
    );
    assert.strictEqual(
        result.resources[1].input.path,
        "resources/bin/ocm-darwin-arm64",
        "Should rewrite second CLI binary path"
    );

    // Image resource should be converted to ociArtifact
    const image = result.resources[2];
    assert.strictEqual(image.type, "ociImage", "Image type should be ociImage");
    assert.strictEqual(image.version, "v1.0.0", "Image version should be updated");
    assert.deepStrictEqual(image.access, {
        type: "ociArtifact",
        imageReference: "ghcr.io/ocm/cli:v1.0.0",
    }, "Image access should have ociArtifact reference");
    assert.strictEqual(image.relation, undefined, "relation should be deleted");
    assert.strictEqual(image.input, undefined, "input should be deleted");
}

// patchCliConstructor: missing image resource
assert.throws(
    () => patchCliConstructor({ resources: [{ name: "cli", input: { type: "file", path: "x" } }] }, "ref", "tag"),
    /no resource named 'image'/,
    "Should throw when image resource is missing"
);

// patchCliConstructor: missing resources array
assert.throws(
    () => patchCliConstructor({ name: "test" }, "ref", "tag"),
    /no 'resources' array/,
    "Should throw when resources array is missing"
);

// patchCliConstructor: non-file CLI resources are left untouched
{
    const constructor = {
        resources: [
            { name: "cli", type: "executable", input: { type: "dir", path: "/some/dir" } },
            { name: "image", type: "ociImage", relation: "local", input: { type: "file", path: "x" } },
        ],
    };
    patchCliConstructor(constructor, "ref", "tag");
    assert.strictEqual(
        constructor.resources[0].input.path,
        "/some/dir",
        "Non-file CLI resources should not be modified"
    );
}

// ----------------------------------------------------------
// parseConstructorFile (round-trip via temp file)
// ----------------------------------------------------------
console.log("Testing parseConstructorFile...");

{
    const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "ocm-test-"));
    const tmpFile = path.join(tmpDir, "component-constructor.yaml");

    const testDoc = { name: "ocm.software/test", version: "0.1.0", provider: { name: "test" } };
    fs.writeFileSync(tmpFile, yaml.dump(testDoc), "utf8");

    const parsed = parseConstructorFile(tmpFile);
    assert.strictEqual(parsed.name, "ocm.software/test", "Should parse name from YAML file");
    assert.strictEqual(parsed.version, "0.1.0", "Should parse version from YAML file");

    // Cleanup
    fs.unlinkSync(tmpFile);
    fs.rmdirSync(tmpDir);
}

// parseConstructorFile: non-existent file
assert.throws(
    () => parseConstructorFile("/nonexistent/file.yaml"),
    /ENOENT/,
    "Should throw for non-existent file"
);

// parseConstructorFile: invalid YAML (empty file)
{
    const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "ocm-test-"));
    const tmpFile = path.join(tmpDir, "empty.yaml");
    fs.writeFileSync(tmpFile, "", "utf8");

    assert.throws(
        () => parseConstructorFile(tmpFile),
        /Invalid constructor file/,
        "Should throw for empty YAML file"
    );

    fs.unlinkSync(tmpFile);
    fs.rmdirSync(tmpDir);
}

console.log("✅ All component-constructor tests passed.");