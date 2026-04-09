/** Kubernetes CRD YAML input types. */

export interface CrdDocument {
  apiVersion: string;
  kind: string;
  metadata: Record<string, unknown>;
  spec: CrdSpec;
}

export interface CrdSpec {
  group: string;
  names: CrdNames;
  scope?: string;
  versions: CrdVersion[];
}

export interface CrdNames {
  kind: string;
  plural: string;
  singular: string;
  listKind?: string;
}

export interface CrdVersion {
  name: string;
  served: boolean;
  storage: boolean;
  schema?: { openAPIV3Schema?: Record<string, unknown> };
}
