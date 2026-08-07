/** JSON Schema input types (JSON Schema / OpenAPI v3 nodes). */

export interface SchemaNode {
  type?: string | string[];
  title?: string;
  description?: string;
  $ref?: string;
  properties?: Record<string, SchemaNode>;
  required?: string[];
  items?: SchemaNode;
  oneOf?: SchemaNode[];
  anyOf?: SchemaNode[];
  enum?: string[];
  const?: string;
  $defs?: Record<string, SchemaNode>;
  "x-kubernetes-validations"?: Array<{ rule?: string }>;
  spec?: { versions?: Array<{ schema?: { openAPIV3Schema?: SchemaNode } }> };
  [key: string]: unknown;
}
