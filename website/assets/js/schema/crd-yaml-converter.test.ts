import { describe, it, before } from "node:test";
import assert from "node:assert/strict";
import { crdYamlToModels, isYamlUrl } from "./crd-yaml-converter.ts";
import type { SchemaField } from "./schema-model.types.ts";

const CRD_YAML_URL = "https://raw.githubusercontent.com/open-component-model/open-component-model/main/kubernetes/controller/config/crd/bases/delivery.ocm.software_components.yaml";

let crdModels: ReturnType<typeof crdYamlToModels>;

before(async () => {
  const resp = await fetch(CRD_YAML_URL);
  assert.ok(resp.ok, `Failed to fetch CRD YAML: ${resp.status}`);
  crdModels = crdYamlToModels(await resp.text());
});

/** Build a minimal CRD YAML string. */
function crd(kind: string, group: string, versions: Array<{name: string; served?: boolean; storage?: boolean; props?: Record<string, string>}>) {
  const versionEntries = versions.map((v) => `
    - name: ${v.name}
      served: ${v.served ?? true}
      storage: ${v.storage ?? true}
      schema:
        openAPIV3Schema:
          type: object
          properties:${Object.entries(v.props || { spec: "object" }).map(
    ([k, t]) => `\n            ${k}:\n              type: ${t}`).join("")}`).join("");

  return `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: ${kind.toLowerCase()}s.${group}
spec:
  group: ${group}
  names:
    kind: ${kind}
  versions:${versionEntries}
`;
}

describe("crdYamlToModels", () => {
  it("real CRD: correct meta and fields", () => {
    assert.ok(crdModels.length >= 1);
    assert.equal(crdModels[0].meta.kind, "Component");
    assert.ok(crdModels[0].meta.apiVersions[0].includes("delivery.ocm.software"));
    assert.ok(crdModels[0].meta.description.length > 0);
    assert.equal(crdModels[0].sections.length, 1);
    assert.equal(crdModels[0].sections[0].title, "Schema");

    const names = crdModels[0].sections[0].fields.map((f) => f.name);
    for (const f of ["apiVersion", "kind", "metadata", "spec", "status"]) {
      assert.ok(names.includes(f), `missing: ${f}`);
    }

    const spec = crdModels[0].sections[0].fields.find((f) => f.name === "spec")!;
    assert.ok(spec.properties!.length > 0);
  });

  it("fields conform to SchemaField shape", () => {
    function check(field: SchemaField) {
      for (const k of ["name", "type", "description"] as const) assert.equal(typeof field[k], "string");
      for (const k of ["required", "immutable"] as const) assert.equal(typeof field[k], "boolean");
      if (field.properties) field.properties.forEach(check);
    }
    crdModels[0].sections[0].fields.forEach(check);
  });

  it("multi-CRD documents", () => {
    const yaml = [
      crd("Foo", "example.com", [{ name: "v1", props: { spec: "object" } }]),
      crd("Bar", "example.com", [{ name: "v1", props: { spec: "object" } }]),
    ].join("---\n");
    const models = crdYamlToModels(yaml);
    assert.equal(models.length, 2);
    assert.equal(models[0].meta.kind, "Foo");
    assert.equal(models[1].meta.kind, "Bar");
  });

  it("multi-version CRDs", () => {
    const yaml = crd("Widget", "example.com", [
      { name: "v1alpha1", storage: false, props: { color: "string" } },
      { name: "v1", props: { color: "string", size: "integer" } },
    ]);
    const models = crdYamlToModels(yaml);
    assert.equal(models.length, 2);
    assert.equal(models[0].meta.apiVersions[0], "example.com/v1alpha1");
    assert.equal(models[1].meta.apiVersions[0], "example.com/v1");
  });

  it("skips non-served versions", () => {
    const yaml = crd("Thing", "example.com", [
      { name: "v1alpha1", served: false, storage: false },
      { name: "v1" },
    ]);
    const models = crdYamlToModels(yaml);
    assert.equal(models.length, 1);
    assert.equal(models[0].meta.apiVersions[0], "example.com/v1");
  });

  it("returns empty for non-CRD YAML", () => {
    assert.equal(crdYamlToModels("apiVersion: v1\nkind: ConfigMap\n").length, 0);
  });
});

describe("isYamlUrl", () => {
  it("detects .yaml", () => assert.equal(isYamlUrl("https://example.com/crd.yaml"), true));
  it("detects .yml", () => assert.equal(isYamlUrl("https://example.com/crd.yml"), true));
  it("rejects .json", () => assert.equal(isYamlUrl("https://example.com/s.json"), false));
});
