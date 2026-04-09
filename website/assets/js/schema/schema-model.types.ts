/** View-model types for the schema renderer. */

export interface SchemaField {
  name: string;
  type: string;
  description: string;
  required: boolean;
  immutable: boolean;
  properties: SchemaField[] | null;
  variants: FieldVariant[] | null;
}

export interface FieldVariant {
  title: string;
  type: string;
  description: string;
  properties: SchemaField[] | null;
}

export interface SchemaSection {
  title: string;
  description: string;
  fields: SchemaField[];
}

export interface SchemaMeta {
  description: string;
  apiVersions: string[];
  kind: string;
}

export interface SchemaModel {
  meta: SchemaMeta;
  sections: SchemaSection[];
}
