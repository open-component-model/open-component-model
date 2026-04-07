/** Converts Kubernetes CRD YAML into SchemaModel(s). Handles multi-doc and multi-version CRDs. */

import yaml from "js-yaml";
import { jsonSchemaToModel } from "./json-schema-converter.ts";
import type { CrdDocument, CrdVersion } from "./crd-yaml-converter.types.ts";
import type { SchemaMeta, SchemaModel } from "./schema-model.types.ts";
import type { SchemaNode } from "./json-schema-converter.types.ts";

/**
 * Extract SchemaMeta from a CRD document + version entry.
 */
function extractCrdMeta(crd: CrdDocument, version: CrdVersion): SchemaMeta {
  const schema = version.schema?.openAPIV3Schema || {};
  return {
    description: (schema as Record<string, unknown>).description as string || "",
    apiVersions: [`${crd.spec.group}/${version.name}`],
    kind: crd.spec.names?.kind || "",
  };
}

/**
 * Convert a single CRD version into a SchemaModel.
 */
function versionToModel(crd: CrdDocument, version: CrdVersion): SchemaModel {
  const schema = version.schema?.openAPIV3Schema;
  if (!schema) {
    return { meta: extractCrdMeta(crd, version), sections: [] };
  }
  const model = jsonSchemaToModel(schema as SchemaNode);
  return { meta: extractCrdMeta(crd, version), sections: model.sections };
}

/** Convert a CRD YAML string into one or more SchemaModels. */
export function crdYamlToModels(text: string): SchemaModel[] {
  const docs = yaml.loadAll(text).filter(Boolean) as Record<string, unknown>[];
  const crds = docs.filter((d) => d.kind === "CustomResourceDefinition") as unknown as CrdDocument[];
  if (!crds.length) return [];

  const models: SchemaModel[] = [];
  for (const crd of crds) {
    const versions = (crd.spec?.versions || []).filter((v) => v.served !== false);
    for (const version of versions) {
      models.push(versionToModel(crd, version));
    }
  }
  return models;
}

/** Check whether a URL points to a YAML file based on its extension. */
export function isYamlUrl(url: string): boolean {
  try {
    return /\.ya?ml$/i.test(url);
  } catch {
    return false;
  }
}
