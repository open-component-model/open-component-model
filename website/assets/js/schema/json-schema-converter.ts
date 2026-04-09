/** Converts a JSON Schema document into a SchemaModel with polymorphism handling. */

import type {SchemaNode} from "./json-schema-converter.types.ts";
import type {SchemaField, FieldVariant, SchemaSection, SchemaMeta, SchemaModel} from "./schema-model.types.ts";

/**
 * Follow a `$ref` pointer. Does NOT collapse oneOf/anyOf.
 */
function resolveRef(node: SchemaNode, root: SchemaNode, seen = new Set<string>()): SchemaNode {
    if (!node || typeof node !== "object") return node || {};

    if (node.$ref) {
        if (seen.has(node.$ref)) return {type: "object", description: "(circular)"};
        seen.add(node.$ref);

        let target: Record<string, unknown> = root as Record<string, unknown>;
        for (const raw of node.$ref.replace("#/", "").split("/")) {
            const p = raw.replace(/~1/g, "/").replace(/~0/g, "~");
            target = target?.[p] as Record<string, unknown>;
            if (!target) return {};
        }

        const {$ref: _, ...siblings} = node;
        return resolveRef({...target, ...siblings} as SchemaNode, root, seen);
    }

    return node;
}

/**
 * Classify oneOf/anyOf as nullable (1 real + null) or polymorphic (2+ real).
 */
function classifyUnion(branches: SchemaNode[]): { kind: "nullable"; resolved: SchemaNode } | {
    kind: "polymorphic";
    branches: SchemaNode[]
} {
    const nonNull = branches.filter((b) => b && b.type !== "null");
    if (nonNull.length === 1) return {kind: "nullable", resolved: nonNull[0]};
    return {kind: "polymorphic", branches: nonNull.length ? nonNull : branches};
}

/**
 * Resolve $ref then handle oneOf/anyOf (collapse nullable, preserve polymorphic).
 */
function resolve(node: SchemaNode, root: SchemaNode, seen = new Set<string>()): SchemaNode {
    const derefed = resolveRef(node, root, new Set(seen));

    for (const kw of ["oneOf", "anyOf"] as const) {
        if (Array.isArray(derefed[kw])) {
            const resolved = (derefed[kw] as SchemaNode[]).map((o) => resolveRef(o, root, new Set(seen)));
            const union = classifyUnion(resolved);
            if (union.kind === "nullable") {
                const {[kw]: _, ...rest} = derefed;
                return resolve({...rest, ...union.resolved} as SchemaNode, root, seen);
            }
            return {...derefed, [kw]: union.branches};
        }
    }

    return derefed;
}

function normalizeType(type: string | string[] | undefined): string {
    if (Array.isArray(type)) return type.find((t) => t !== "null") || type[0] || "object";
    return type || "object";
}

/**
 * Merge shared parent properties into a oneOf/anyOf branch.
 */
function mergeParentInto(branch: SchemaNode, parent: SchemaNode, _kw: "oneOf" | "anyOf"): SchemaNode {
    if (!parent.properties && !parent.required) return branch;
    return {
        ...branch,
        properties: {...parent.properties, ...branch.properties},
        required: [...(parent.required || []), ...(branch.required || [])],
    };
}

/**
 * Derive a variant title from a distinguishing required property, type, or index.
 */
function variantTitle(branch: SchemaNode, index: number): string {
    if (branch.required?.length) {
        const distinctive = branch.required.find((r) => !["type", "name", "version"].includes(r));
        if (distinctive) return distinctive;
    }
    const t = normalizeType(branch.type);
    return t !== "object" ? t : `Variant ${index + 1}`;
}

/**
 * Convert a schema node's properties into SchemaField[].
 */
function fieldsFrom(node: SchemaNode, root: SchemaNode, seen: Set<string>): SchemaField[] {
    const props = node?.properties || {};
    const req = node?.required || [];
    return Object.entries(props).map(([k, v]) => convertField(k, v, req, root, seen));
}

/**
 * Convert a single schema property into a SchemaField.
 */
function convertField(name: string, raw: SchemaNode, requiredList: string[], root: SchemaNode, seen = new Set<string>()): SchemaField {
    const prop = resolve(raw, root, new Set(seen));
    const immutable = prop["x-kubernetes-validations"]?.some((v: {
        rule?: string
    }) => v.rule?.includes("== oldSelf")) || false;
    const required = requiredList.includes(name);

    // Polymorphic oneOf/anyOf
    for (const kw of ["oneOf", "anyOf"] as const) {
        if (Array.isArray(prop[kw])) {
            const hasSharedProps = !!prop.properties;
            const variants: FieldVariant[] = (prop[kw] as SchemaNode[]).map((branch, i) => {
                const merged = mergeParentInto(branch, prop, kw);
                const resolved = resolve(merged, root, new Set(seen));
                const title = variantTitle(resolved, i);
                const fields = resolved.properties ? fieldsFrom(resolved, root, new Set(seen)) : null;
                return {
                    title, type: normalizeType(resolved.type), description: resolved.description || "",
                    properties: hasSharedProps ? fields?.filter((f) => f.name !== title) || null : fields,
                };
            });
            return {
                name, type: normalizeType(prop.type), description: prop.description || "",
                required, immutable, properties: null, variants,
            };
        }
    }

    // Array with items
    if (prop.type === "array" && prop.items) {
        const items = resolve(prop.items, root, new Set(seen));

        for (const kw of ["oneOf", "anyOf"] as const) {
            if (Array.isArray(items[kw])) {
                const hasSharedItemProps = !!items.properties;
                const variants: FieldVariant[] = (items[kw] as SchemaNode[]).map((branch, i) => {
                    const merged = mergeParentInto(branch, items, kw);
                    const resolved = resolve(merged, root, new Set(seen));
                    const title = variantTitle(resolved, i);
                    const fields = resolved.properties ? fieldsFrom(resolved, root, new Set(seen)) : null;
                    return {
                        title, type: `[]${normalizeType(resolved.type)}`, description: resolved.description || "",
                        properties: hasSharedItemProps ? fields?.filter((f) => f.name !== title) || null : fields,
                    };
                });
                return {
                    name, type: `[]${normalizeType(items.type)}`, description: prop.description || "",
                    required, immutable, properties: null, variants,
                };
            }
        }

        return {
            name, type: `[]${normalizeType(items.type)}`, description: prop.description || "",
            required, immutable, variants: null,
            properties: items.properties ? fieldsFrom(items, root, new Set(seen)) : null,
        };
    }

    // Plain object or scalar
    return {
        name, type: normalizeType(prop.type), description: prop.description || "",
        required, immutable, variants: null,
        properties: prop.properties ? fieldsFrom(prop, root, new Set(seen)) : null,
    };
}

/**
 * Extract SchemaMeta from apiVersion/kind property definitions.
 */
function extractMeta(schemaRoot: SchemaNode): SchemaMeta {
    const resolved = schemaRoot?.properties ? schemaRoot : resolve(schemaRoot, schemaRoot);
    const props = resolved?.properties || {};
    const av = resolveRef(props.apiVersion || {}, schemaRoot);
    const kind = resolveRef(props.kind || {}, schemaRoot);

    return {
        description: schemaRoot.description || "",
        apiVersions: av.enum || (av["const"] ? [av["const"]] : []),
        kind: kind["const"] || (kind.enum ? kind.enum.join(", ") : ""),
    };
}

/**
 * Unwrap CRD-style openAPIV3Schema or return as-is.
 */
function getSchemaRoot(data: SchemaNode): SchemaNode {
    return data?.spec?.versions?.[0]?.schema?.openAPIV3Schema as SchemaNode || data;
}

/**
 * Convert a parsed JSON Schema document into a SchemaModel.
 */
export function jsonSchemaToModel(data: SchemaNode): SchemaModel {
    const root = getSchemaRoot(data);

    if (root?.properties) {
        return {
            meta: extractMeta(root),
            sections: [{title: "Fields", description: "", fields: fieldsFrom(root, root, new Set())}],
        };
    }

    for (const kw of ["oneOf", "anyOf"] as const) {
        if (Array.isArray(root?.[kw])) {
            const sections: SchemaSection[] = (root[kw] as SchemaNode[]).map((option, i) => {
                const resolved = resolve(option, root);
                return {
                    title: option.title || resolved.title || `Variant ${i + 1}`,
                    description: option.description || "",
                    fields: fieldsFrom(resolved, root, new Set()),
                };
            });
            return {meta: extractMeta(root), sections};
        }
    }

    return {meta: extractMeta(root), sections: []};
}
