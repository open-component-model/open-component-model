import { describe, it, before } from "node:test";
import assert from "node:assert/strict";
import { jsonSchemaToModel } from "./json-schema-converter.ts";
import type { SchemaField } from "./schema-model.types.ts";

const DESCRIPTOR_URL = "https://raw.githubusercontent.com/open-component-model/open-component-model/main/bindings/go/descriptor/v2/resources/schema-2020-12.json";
const CONSTRUCTOR_URL = "https://raw.githubusercontent.com/open-component-model/open-component-model/main/bindings/go/constructor/spec/v1/resources/schema-2020-12.json";

let descriptorModel: ReturnType<typeof jsonSchemaToModel>;
let constructorModel: ReturnType<typeof jsonSchemaToModel>;

before(async () => {
  const [descResp, consResp] = await Promise.all([fetch(DESCRIPTOR_URL), fetch(CONSTRUCTOR_URL)]);
  assert.ok(descResp.ok, `Failed to fetch descriptor: ${descResp.status}`);
  assert.ok(consResp.ok, `Failed to fetch constructor: ${consResp.status}`);
  descriptorModel = jsonSchemaToModel(await descResp.json());
  constructorModel = jsonSchemaToModel(await consResp.json());
});

/** Recursively assert all fields match the SchemaField shape. */
function checkFieldShape(field: SchemaField) {
  assert.equal(typeof field.name, "string");
  assert.equal(typeof field.type, "string");
  assert.equal(typeof field.description, "string");
  assert.equal(typeof field.required, "boolean");
  assert.equal(typeof field.immutable, "boolean");
  if (field.properties) field.properties.forEach(checkFieldShape);
  if (field.variants) {
    field.variants.forEach((v) => {
      assert.equal(typeof v.title, "string");
      assert.equal(typeof v.type, "string");
      assert.equal(typeof v.description, "string");
      if (v.properties) v.properties.forEach(checkFieldShape);
    });
  }
}

describe("descriptor schema", () => {
  it("single section with expected fields", () => {
    assert.equal(descriptorModel.sections.length, 1);
    assert.equal(descriptorModel.sections[0].title, "Schema");
    const names = descriptorModel.sections[0].fields.map((f) => f.name);
    for (const f of ["meta", "component", "signatures", "nestedDigests"]) {
      assert.ok(names.includes(f), `missing: ${f}`);
    }
  });

  it("resolves nested $ref (meta.schemaVersion, component.resources)", () => {
    const meta = descriptorModel.sections[0].fields.find((f) => f.name === "meta")!;
    assert.ok(meta.properties!.length > 0);
    assert.ok(meta.properties!.some((f) => f.name === "schemaVersion"));

    const component = descriptorModel.sections[0].fields.find((f) => f.name === "component")!;
    const resources = component.properties!.find((f) => f.name === "resources")!;
    assert.match(resources.type, /\[\]/);
    assert.ok(resources.properties!.length > 0);
  });

  it("nullable oneOf collapses without variants (digest)", () => {
    const component = descriptorModel.sections[0].fields.find((f) => f.name === "component")!;
    const digest = component.properties!.find((f) => f.name === "resources")!
      .properties!.find((f) => f.name === "digest")!;
    assert.equal(digest.type, "object");
    assert.ok(digest.properties!.length > 0);
    assert.equal(digest.variants, null);
  });

  it("all fields conform to SchemaField shape", () => {
    descriptorModel.sections[0].fields.forEach(checkFieldShape);
  });

  it("meta has description", () => {
    assert.ok(descriptorModel.meta.description.length > 0);
  });
});

describe("constructor schema", () => {
  it("two variant sections from root oneOf", () => {
    assert.equal(constructorModel.sections.length, 2);
    assert.match(constructorModel.sections[0].title, /Variant 1/);
    assert.match(constructorModel.sections[1].title, /Variant 2/);
  });

  it("variant 1: components array", () => {
    const fields = constructorModel.sections[0].fields;
    assert.equal(fields.length, 1);
    assert.equal(fields[0].name, "components");
    assert.match(fields[0].type, /\[\]/);
  });

  it("variant 2: direct component fields with polymorphic resources/sources", () => {
    const names = constructorModel.sections[1].fields.map((f) => f.name);
    for (const f of ["name", "version", "provider", "resources"]) {
      assert.ok(names.includes(f), `missing: ${f}`);
    }

    const resources = constructorModel.sections[1].fields.find((f) => f.name === "resources")!;
    assert.ok(resources.variants!.length >= 2, "resources should have access/input variants");

    const sources = constructorModel.sections[1].fields.find((f) => f.name === "sources")!;
    assert.ok(sources.variants!.length >= 2, "sources should have access/input variants");
  });

  it("variants include shared fields from parent", () => {
    const sources = constructorModel.sections[1].fields.find((f) => f.name === "sources")!;
    for (const v of sources.variants!) {
      const names = v.properties!.map((f) => f.name);
      assert.ok(names.includes("name"), `variant "${v.title}" missing shared "name"`);
      assert.ok(names.includes("type"), `variant "${v.title}" missing shared "type"`);
    }
  });

  it("meta has description", () => {
    assert.ok(constructorModel.meta.description.length > 0);
  });
});

describe("edge cases", () => {
  it("empty schema", () => {
    const model = jsonSchemaToModel({});
    assert.ok(model.meta);
    assert.equal(model.sections.length, 0);
  });

  it("simple properties", () => {
    const model = jsonSchemaToModel({
      type: "object",
      properties: { foo: { type: "string", description: "a foo" } },
    });
    assert.equal(model.sections[0].fields[0].name, "foo");
    assert.equal(model.sections[0].fields[0].type, "string");
  });

  it("immutable via x-kubernetes-validations", () => {
    const model = jsonSchemaToModel({
      type: "object",
      properties: {
        name: { type: "string", "x-kubernetes-validations": [{ rule: "self == oldSelf" }] },
      },
    });
    assert.equal(model.sections[0].fields[0].immutable, true);
  });

  it("required flag", () => {
    const model = jsonSchemaToModel({
      type: "object", required: ["a"],
      properties: { a: { type: "string" }, b: { type: "string" } },
    });
    assert.equal(model.sections[0].fields.find((f) => f.name === "a")!.required, true);
    assert.equal(model.sections[0].fields.find((f) => f.name === "b")!.required, false);
  });

  it("circular $ref", () => {
    const model = jsonSchemaToModel({
      type: "object",
      $defs: { loop: { $ref: "#/$defs/loop" } },
      properties: { x: { $ref: "#/$defs/loop" } },
    });
    assert.equal(model.sections[0].fields.find((f) => f.name === "x")!.type, "object");
  });

  it("polymorphic oneOf → variants (no shared props)", () => {
    const model = jsonSchemaToModel({
      type: "object",
      properties: {
        access: {
          oneOf: [
            { type: "object", required: ["localPath"], properties: { localPath: { type: "string" } } },
            { type: "object", required: ["imageRef"], properties: { imageRef: { type: "string" } } },
          ],
        },
      },
    });
    const access = model.sections[0].fields.find((f) => f.name === "access")!;
    assert.equal(access.properties, null);
    assert.equal(access.variants!.length, 2);
    assert.equal(access.variants![0].title, "localPath");
    assert.equal(access.variants![1].title, "imageRef");
  });

  it("polymorphic oneOf with shared parent props merges and filters", () => {
    const model = jsonSchemaToModel({
      type: "object",
      properties: {
        source: {
          type: "object",
          properties: { name: { type: "string" }, type: { type: "string" } },
          required: ["name"],
          oneOf: [
            { type: "object", required: ["access"], properties: { access: { type: "object" } } },
            { type: "object", required: ["input"], properties: { input: { type: "object" } } },
          ],
        },
      },
    });
    const source = model.sections[0].fields.find((f) => f.name === "source")!;
    assert.equal(source.variants!.length, 2);
    for (const v of source.variants!) {
      const names = v.properties!.map((f) => f.name);
      assert.ok(names.includes("name"), `shared "name" missing in ${v.title}`);
      assert.ok(names.includes("type"), `shared "type" missing in ${v.title}`);
    }
    // Distinguishing props filtered out (redundant with tab title)
    assert.ok(!source.variants![0].properties!.some((f) => f.name === "access"));
    assert.ok(!source.variants![1].properties!.some((f) => f.name === "input"));
  });

  it("nullable oneOf collapses without variants", () => {
    const model = jsonSchemaToModel({
      type: "object",
      properties: {
        label: { oneOf: [{ type: "null" }, { type: "string", description: "a label" }] },
      },
    });
    const label = model.sections[0].fields.find((f) => f.name === "label")!;
    assert.equal(label.type, "string");
    assert.equal(label.description, "a label");
    assert.equal(label.variants, null);
  });
});
